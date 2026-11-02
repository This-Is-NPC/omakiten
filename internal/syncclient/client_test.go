package syncclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/syncclient"
	"omakiten/internal/syncserver"
)

const (
	token  = "secret"
	proj   = domain.UID("01900000-0000-7000-8000-000000000001")
	device = domain.UID("01900000-0000-7000-8000-00000000000a")
)

func realServer(t *testing.T) string {
	t.Helper()
	store, err := syncserver.Open(context.Background(), filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(syncserver.NewHandler(store, []string{token}))
	t.Cleanup(func() {
		srv.Close()
		_ = store.Close()
	})
	return srv.URL
}

func newClient(t *testing.T, url, tok string) *syncclient.Client {
	t.Helper()
	c, err := syncclient.New(url, tok, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func change(n int) domain.SyncChange {
	row := "01900000-0000-7000-8000-0000000001" + strconv.Itoa(10+n)
	return domain.SyncChange{
		Table:  "tasks",
		RowUID: row,
		Op:     domain.SyncOpUpsert,
		HLC:    domain.HLC{Wall: int64(10 + n), Device: device}.String(),
		Device: device,
		Data:   map[string]any{"title": row},
	}
}

func wantCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}

func TestListPushAndPagedPull(t *testing.T) {
	c := newClient(t, realServer(t), token)
	ctx := context.Background()
	info := domain.SyncProjectInfo{UID: proj, Name: "P", Slug: "p", KitKey: "k"}
	changes := []domain.SyncChange{change(1), change(2), change(3)}

	cursor, err := c.Push(ctx, info, changes)
	if err != nil || cursor != 3 {
		t.Fatalf("push = %d, %v", cursor, err)
	}
	projects, err := c.ListProjects(ctx)
	if err != nil || len(projects) != 1 || projects[0].UID != proj {
		t.Fatalf("list = %+v, %v", projects, err)
	}
	got, cursor, more, err := c.Pull(ctx, proj, 0, 2)
	if err != nil || len(got) != 2 || !more || cursor != 2 {
		t.Fatalf("page 1 = %d changes, cursor %d, more %v, %v", len(got), cursor, more, err)
	}
	got, cursor, more, err = c.Pull(ctx, proj, cursor, 2)
	if err != nil || len(got) != 1 || more || cursor != 3 || got[0].RowUID != changes[2].RowUID {
		t.Fatalf("page 2 = %+v, cursor %d, more %v, %v", got, cursor, more, err)
	}
}

func TestBadTokenIsDenied(t *testing.T) {
	url := realServer(t)
	_, err := newClient(t, url, "wrong").ListProjects(context.Background())
	wantCode(t, err, domain.ErrOperationDenied)
	if strings.Contains(err.Error(), "wrong") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestUnknownProject(t *testing.T) {
	_, _, _, err := newClient(t, realServer(t), token).Pull(context.Background(), proj, 0, 10)
	wantCode(t, err, domain.ErrProjectNotFound)
}

func TestServerValidationError(t *testing.T) {
	bad := change(1)
	bad.Table = "sync_identity"
	_, err := newClient(t, realServer(t), token).Push(context.Background(),
		domain.SyncProjectInfo{UID: proj, Name: "P", Slug: "p", KitKey: "k"}, []domain.SyncChange{bad})
	wantCode(t, err, domain.ErrValidation)
}

func TestNewRefusesBadURL(t *testing.T) {
	for _, u := range []string{"", "://x", "ftp://host", "localhost:8080", "http://", "/relative"} {
		_, err := syncclient.New(u, token, nil)
		wantCode(t, err, domain.ErrValidation)
	}
}

func TestNonJSONFailureCarriesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "<html>bad gateway</html>", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	_, err := newClient(t, srv.URL, token).ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 502") || !strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownEnvelopeCodeCarriesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"code":"internal_error","msg":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	_, err := newClient(t, srv.URL, token).ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectionErrorNamesServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := newClient(t, url, token).ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), url) || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newClient(t, realServer(t), token).ListProjects(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
