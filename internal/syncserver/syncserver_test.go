package syncserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/syncproto"
)

const (
	token  = "secret"
	proj   = domain.UID("01900000-0000-7000-8000-000000000001")
	device = domain.UID("01900000-0000-7000-8000-00000000000a")
)

type env struct {
	t     *testing.T
	path  string
	store *Store
	srv   *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, path: filepath.Join(t.TempDir(), "sync.db")}
	e.open()
	return e
}

func (e *env) open() {
	store, err := Open(context.Background(), e.path)
	if err != nil {
		e.t.Fatal(err)
	}
	e.store = store
	e.srv = httptest.NewServer(NewHandler(store, []string{"other", token}))
	e.t.Cleanup(e.close)
}

func (e *env) close() {
	if e.srv != nil {
		e.srv.Close()
		_ = e.store.Close()
		e.srv = nil
	}
}

func (e *env) reopen() {
	e.close()
	e.open()
}

func (e *env) do(method, path string, body any, auth string) (int, []byte) {
	e.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func (e *env) push(changes ...domain.SyncChange) syncproto.PushResponse {
	e.t.Helper()
	status, raw := e.do(http.MethodPost, syncproto.ChangesPath(proj), pushBody(changes), "Bearer "+token)
	if status != http.StatusOK {
		e.t.Fatalf("push status %d: %s", status, raw)
	}
	var resp syncproto.PushResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		e.t.Fatal(err)
	}
	return resp
}

func (e *env) pull(query string) syncproto.PullResponse {
	e.t.Helper()
	status, raw := e.do(http.MethodGet, syncproto.ChangesPath(proj)+"?"+query, nil, "Bearer "+token)
	if status != http.StatusOK {
		e.t.Fatalf("pull status %d: %s", status, raw)
	}
	var resp syncproto.PullResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		e.t.Fatal(err)
	}
	return resp
}

func pushBody(changes []domain.SyncChange) syncproto.PushRequest {
	return syncproto.PushRequest{
		Project: domain.SyncProjectInfo{UID: proj, Name: "P", Slug: "p", KitKey: "k"},
		Changes: changes,
	}
}

func change(row string, wall int64) domain.SyncChange {
	return domain.SyncChange{
		Table:  "tasks",
		RowUID: row,
		Op:     domain.SyncOpUpsert,
		HLC:    domain.HLC{Wall: wall, Device: device}.String(),
		Device: device,
		Data:   map[string]any{"title": row + "@" + strconv.FormatInt(wall, 10)},
	}
}

func rowName(i int) string {
	return "01900000-0000-7000-8000-0000000001" + strconv.Itoa(10+i)
}

func TestAuth(t *testing.T) {
	e := newEnv(t)
	for _, auth := range []string{"", "Bearer wrong", "secret", "Basic secret"} {
		status, raw := e.do(http.MethodGet, syncproto.ProjectsPath, nil, auth)
		if status != http.StatusUnauthorized {
			t.Fatalf("auth %q: status %d", auth, status)
		}
		var body errorBody
		if err := json.Unmarshal(raw, &body); err != nil || body.OK || body.Code != codeUnauthorized {
			t.Fatalf("auth %q: body %s", auth, raw)
		}
	}
	for _, tok := range []string{"secret", "other"} {
		if status, _ := e.do(http.MethodGet, syncproto.ProjectsPath, nil, "Bearer "+tok); status != http.StatusOK {
			t.Fatalf("token %s: status %d", tok, status)
		}
	}
}

func TestPushThenPullInSeqOrder(t *testing.T) {
	e := newEnv(t)
	resp := e.push(change(rowName(1), 10), change(rowName(2), 10), change(rowName(3), 10))
	if resp.Accepted != 3 || resp.Cursor != 3 {
		t.Fatalf("push = %+v", resp)
	}
	got := e.pull("after=0")
	if len(got.Changes) != 3 || got.Cursor != 3 || got.More || got.Project.Slug != "p" {
		t.Fatalf("pull = %+v", got)
	}
	for i, c := range got.Changes {
		if c.RowUID != rowName(i+1) || c.Data["title"] == nil {
			t.Fatalf("change %d = %+v", i, c)
		}
	}
	if empty := e.pull("after=3"); len(empty.Changes) != 0 || empty.Cursor != 3 {
		t.Fatalf("pull after end = %+v", empty)
	}
}

func TestServerLastWriterWins(t *testing.T) {
	e := newEnv(t)
	e.push(change(rowName(1), 10))
	if r := e.push(change(rowName(1), 5)); r.Accepted != 0 || r.Cursor != 1 {
		t.Fatalf("older = %+v", r)
	}
	if r := e.push(change(rowName(1), 10)); r.Accepted != 0 {
		t.Fatalf("equal = %+v", r)
	}
	if r := e.push(change(rowName(1), 20)); r.Accepted != 1 || r.Cursor != 2 {
		t.Fatalf("newer = %+v", r)
	}
	got := e.pull("after=0")
	if len(got.Changes) != 1 || got.Cursor != 2 || got.Changes[0].Data["title"] != rowName(1)+"@20" {
		t.Fatalf("pull = %+v", got)
	}
	del := change(rowName(1), 30)
	del.Op, del.Data = domain.SyncOpDelete, nil
	e.push(del)
	if got := e.pull("after=2"); len(got.Changes) != 1 || got.Changes[0].Op != domain.SyncOpDelete {
		t.Fatalf("tombstone pull = %+v", got)
	}
}

func TestPullPagingAndLimitClamp(t *testing.T) {
	e := newEnv(t)
	var changes []domain.SyncChange
	for i := range 5 {
		changes = append(changes, change(rowName(i), 10))
	}
	e.push(changes...)
	first := e.pull("after=0&limit=2")
	if len(first.Changes) != 2 || !first.More || first.Cursor != 2 {
		t.Fatalf("first page = %+v", first)
	}
	second := e.pull("after=2&limit=2")
	if len(second.Changes) != 2 || !second.More || second.Cursor != 4 {
		t.Fatalf("second page = %+v", second)
	}
	last := e.pull("after=4&limit=2")
	if len(last.Changes) != 1 || last.More || last.Cursor != 5 {
		t.Fatalf("last page = %+v", last)
	}
	if all := e.pull("limit=" + strconv.Itoa(syncproto.MaxPullLimit*10)); len(all.Changes) != 5 || all.More {
		t.Fatalf("clamped pull = %+v", all)
	}
	got, err := e.store.Pull(context.Background(), proj, 0, syncproto.MaxPullLimit*10)
	if err != nil || len(got.Changes) != 5 {
		t.Fatalf("store clamp = %+v, %v", got, err)
	}
}

func TestErrors(t *testing.T) {
	e := newEnv(t)
	bad := change(rowName(1), 10)
	bad.Table = "sync_identity"
	mismatch := pushBody(nil)
	mismatch.Project.UID = device
	tests := []struct {
		name   string
		method string
		path   string
		body   any
		status int
		code   string
	}{
		{"unknown project pull", http.MethodGet, syncproto.ChangesPath(proj), nil, http.StatusNotFound, string(domain.ErrProjectNotFound)},
		{"invalid change", http.MethodPost, syncproto.ChangesPath(proj), pushBody([]domain.SyncChange{bad}), http.StatusBadRequest, string(domain.ErrValidation)},
		{"uid mismatch", http.MethodPost, syncproto.ChangesPath(proj), mismatch, http.StatusBadRequest, string(domain.ErrValidation)},
		{"bad path uid", http.MethodGet, syncproto.ProjectsPath + "/nope/changes", nil, http.StatusBadRequest, string(domain.ErrValidation)},
		{"bad body", http.MethodPost, syncproto.ChangesPath(proj), map[string]any{"nope": 1}, http.StatusBadRequest, codeInvalidBody},
		{"bad after", http.MethodGet, syncproto.ChangesPath(proj) + "?after=-1", nil, http.StatusBadRequest, codeInvalidParameter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, raw := e.do(tt.method, tt.path, tt.body, "Bearer "+token)
			var body errorBody
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if status != tt.status || body.OK || body.Code != tt.code || body.Message == "" {
				t.Fatalf("status %d body %s", status, raw)
			}
		})
	}
	if got := e.pull0(); got != http.StatusNotFound {
		t.Fatalf("failed pushes must not create the project, pull = %d", got)
	}
}

func (e *env) pull0() int {
	status, _ := e.do(http.MethodGet, syncproto.ChangesPath(proj), nil, "Bearer "+token)
	return status
}

func TestListProjects(t *testing.T) {
	e := newEnv(t)
	status, raw := e.do(http.MethodGet, syncproto.ProjectsPath, nil, "Bearer "+token)
	if status != http.StatusOK || string(bytes.TrimSpace(raw)) != `{"projects":[]}` {
		t.Fatalf("empty list = %d %s", status, raw)
	}
	e.push(change(rowName(1), 10))
	_, raw = e.do(http.MethodGet, syncproto.ProjectsPath, nil, "Bearer "+token)
	var list syncproto.ProjectsResponse
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Projects) != 1 || list.Projects[0].UID != proj || list.Projects[0].Name != "P" {
		t.Fatalf("list = %s, %v", raw, err)
	}
}

func TestReopenKeepsDataAndSeqMonotonic(t *testing.T) {
	e := newEnv(t)
	e.push(change(rowName(1), 10), change(rowName(2), 10))
	e.reopen()
	if got := e.pull("after=0"); len(got.Changes) != 2 || got.Cursor != 2 {
		t.Fatalf("after reopen = %+v", got)
	}
	if r := e.push(change(rowName(3), 10)); r.Accepted != 1 || r.Cursor != 3 {
		t.Fatalf("push after reopen = %+v", r)
	}
}
