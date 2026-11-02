package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

type fakeSyncStore struct {
	projects []domain.SyncProject
	pending  []domain.SyncChange
	queued   map[int64][]domain.SyncChange
	pushed   []string
	applied  []appliedPage
	cloned   []domain.SyncProjectInfo
	url      string
}

type appliedPage struct {
	n      int
	cursor int64
}

func (f *fakeSyncStore) SyncedProjects(context.Context) ([]domain.SyncProject, error) {
	return f.projects, nil
}

func (f *fakeSyncStore) PendingChanges(_ context.Context, id int64, limit int) ([]domain.SyncChange, error) {
	if f.queued != nil {
		pending := f.queued[id]
		f.queued[id] = nil
		return pending, nil
	}
	return f.pending[:min(limit, len(f.pending))], nil
}

func (f *fakeSyncStore) MarkPushed(_ context.Context, _ int64, hlc string) error {
	f.pushed = append(f.pushed, hlc)
	for i, c := range f.pending {
		if c.HLC == hlc {
			f.pending = f.pending[i+1:]
			break
		}
	}
	return nil
}

func (f *fakeSyncStore) ApplyChanges(_ context.Context, _ int64, changes []domain.SyncChange, cursor int64) error {
	f.applied = append(f.applied, appliedPage{len(changes), cursor})
	return nil
}

func (f *fakeSyncStore) CreateClonedProject(_ context.Context, info domain.SyncProjectInfo, _ string) (domain.Project, error) {
	f.cloned = append(f.cloned, info)
	return domain.Project{ID: 99, Slug: info.Slug}, nil
}

func (f *fakeSyncStore) SyncRemoteURL(context.Context) (string, error) { return f.url, nil }

func (f *fakeSyncStore) SetSyncRemoteURL(_ context.Context, u string) error {
	f.url = u
	return nil
}

type fakeSyncKits struct {
	byProject map[int64]string
	atRoot    string
}

func (k fakeSyncKits) KitKeyOf(_ context.Context, id int64) (string, error) {
	return k.byProject[id], nil
}

func (k fakeSyncKits) KitKeyAt(context.Context, string) (string, error) { return k.atRoot, nil }

type pullPage struct {
	n      int
	cursor int64
	more   bool
}

type fakeSyncRemote struct {
	log      *[]string
	projects []domain.SyncProjectInfo
	pushes   []int
	pushed   []domain.SyncProjectInfo
	pages    []pullPage
	pullsAt  []int64
	pullErr  error
}

func (r *fakeSyncRemote) ListProjects(context.Context) ([]domain.SyncProjectInfo, error) {
	return r.projects, nil
}

func (r *fakeSyncRemote) Push(_ context.Context, info domain.SyncProjectInfo, changes []domain.SyncChange) (int64, error) {
	r.pushed = append(r.pushed, info)
	*r.log = append(*r.log, "push")
	r.pushes = append(r.pushes, len(changes))
	return 1, nil
}

func (r *fakeSyncRemote) Pull(_ context.Context, _ domain.UID, after int64, _ int) ([]domain.SyncChange, int64, bool, error) {
	*r.log = append(*r.log, "pull")
	r.pullsAt = append(r.pullsAt, after)
	if r.pullErr != nil {
		return nil, 0, false, r.pullErr
	}
	if len(r.pages) == 0 {
		return nil, after, false, nil
	}
	p := r.pages[0]
	r.pages = r.pages[1:]
	return make([]domain.SyncChange, p.n), p.cursor, p.more, nil
}

func hlcChanges(n int) []domain.SyncChange {
	out := make([]domain.SyncChange, n)
	for i := range out {
		out[i].HLC = fmt.Sprintf("h%04d", i)
	}
	return out
}

func newSyncFixture(projects ...domain.SyncProject) (*SyncService, *fakeSyncStore, *fakeSyncRemote, *[]string) {
	log := &[]string{}
	store := &fakeSyncStore{projects: projects}
	remote := &fakeSyncRemote{log: log}
	return NewSyncService(store, remote, fakeSyncKits{byProject: map[int64]string{1: "omakase", 2: "lean"}, atRoot: "omakase"}), store, remote, log
}

func syncProject(id int64, slug string) domain.SyncProject {
	return domain.SyncProject{ID: id, Info: domain.SyncProjectInfo{UID: domain.NewUID(), Slug: slug}}
}

func TestSyncPushesBeforePulling(t *testing.T) {
	svc, store, remote, log := newSyncFixture(syncProject(1, "a"))
	store.pending = hlcChanges(3)
	remote.pages = []pullPage{{n: 2, cursor: 7}}
	report, err := svc.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"push", "pull"}; !reflect.DeepEqual(*log, want) {
		t.Fatalf("order = %v, want %v", *log, want)
	}
	want := []domain.SyncProjectReport{{Slug: "a", Pushed: 3, Pulled: 2}}
	if !reflect.DeepEqual(report.Projects, want) {
		t.Fatalf("report = %v, want %v", report.Projects, want)
	}
	if want := []appliedPage{{2, 7}}; !reflect.DeepEqual(store.applied, want) {
		t.Fatalf("applied = %v, want %v", store.applied, want)
	}
}

func TestSyncPushPaging(t *testing.T) {
	svc, store, remote, _ := newSyncFixture(syncProject(1, "a"))
	store.pending = hlcChanges(syncPushPage + 1)
	report, err := svc.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{syncPushPage, 1}; !reflect.DeepEqual(remote.pushes, want) {
		t.Fatalf("pushes = %v, want %v", remote.pushes, want)
	}
	if want := []string{fmt.Sprintf("h%04d", syncPushPage-1), fmt.Sprintf("h%04d", syncPushPage)}; !reflect.DeepEqual(store.pushed, want) {
		t.Fatalf("marked = %v, want %v", store.pushed, want)
	}
	if report.Projects[0].Pushed != syncPushPage+1 {
		t.Fatalf("pushed = %d", report.Projects[0].Pushed)
	}
}

func TestSyncPullPagingFollowsMore(t *testing.T) {
	svc, store, remote, _ := newSyncFixture(domain.SyncProject{ID: 1, PullCursor: 4, Info: domain.SyncProjectInfo{Slug: "a"}})
	remote.pages = []pullPage{{n: 2, cursor: 6, more: true}, {n: 1, cursor: 7}}
	report, err := svc.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{4, 6}; !reflect.DeepEqual(remote.pullsAt, want) {
		t.Fatalf("pull cursors = %v, want %v", remote.pullsAt, want)
	}
	if want := []appliedPage{{2, 6}, {1, 7}}; !reflect.DeepEqual(store.applied, want) {
		t.Fatalf("applied = %v, want %v", store.applied, want)
	}
	if report.Projects[0].Pulled != 3 {
		t.Fatalf("pulled = %d", report.Projects[0].Pulled)
	}
}

func TestSyncSkipsApplyWhenNothingMoved(t *testing.T) {
	svc, store, _, _ := newSyncFixture(syncProject(1, "a"))
	if _, err := svc.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(store.applied) != 0 {
		t.Fatalf("applied = %v, want none", store.applied)
	}
}

func TestSyncSingleProjectSelection(t *testing.T) {
	svc, _, _, _ := newSyncFixture(syncProject(1, "a"), syncProject(2, "b"))
	id := int64(2)
	report, err := svc.Sync(context.Background(), &id)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Projects) != 1 || report.Projects[0].Slug != "b" {
		t.Fatalf("report = %v", report.Projects)
	}
	all, err := svc.Sync(context.Background(), nil)
	if err != nil || len(all.Projects) != 2 || all.Projects[0].Slug != "a" {
		t.Fatalf("all = %v, err = %v", all.Projects, err)
	}
}

func TestSyncUnknownProject(t *testing.T) {
	svc, _, _, _ := newSyncFixture(syncProject(1, "a"))
	id := int64(5)
	_, err := svc.Sync(context.Background(), &id)
	assertCodedError(t, err, domain.ErrProjectNotFound)
}

func TestSyncWrapsErrorWithSlug(t *testing.T) {
	svc, _, remote, _ := newSyncFixture(syncProject(1, "alpha"))
	remote.pullErr = errors.New("boom")
	_, err := svc.Sync(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "alpha") || !errors.Is(err, remote.pullErr) {
		t.Fatalf("err = %v", err)
	}
}

func TestSyncNilRemote(t *testing.T) {
	svc := NewSyncService(&fakeSyncStore{}, nil, fakeSyncKits{})
	ctx := context.Background()
	_, syncErr := svc.Sync(ctx, nil)
	_, cloneErr := svc.Clone(ctx, domain.NewUID(), "/x")
	_, listErr := svc.RemoteProjects(ctx)
	for _, err := range []error{syncErr, cloneErr, listErr} {
		assertCodedError(t, err, domain.ErrValidation)
		if !strings.Contains(err.Error(), "okt remote set") {
			t.Fatalf("err = %v, want mention of okt remote set", err)
		}
	}
}

func TestCloneCreatesProjectThenPulls(t *testing.T) {
	svc, store, remote, log := newSyncFixture()
	info := domain.SyncProjectInfo{UID: domain.NewUID(), Slug: "r"}
	remote.projects = []domain.SyncProjectInfo{{UID: domain.NewUID()}, info}
	remote.pages = []pullPage{{n: 3, cursor: 9}}
	root := t.TempDir()
	project, err := svc.Clone(context.Background(), info.UID, root)
	if err != nil {
		t.Fatal(err)
	}
	if project.ID != 99 || !reflect.DeepEqual(store.cloned, []domain.SyncProjectInfo{info}) {
		t.Fatalf("project = %v, cloned = %v", project, store.cloned)
	}
	if want := []string{"pull"}; !reflect.DeepEqual(*log, want) {
		t.Fatalf("remote calls = %v, want %v", *log, want)
	}
	if want := []appliedPage{{3, 9}}; !reflect.DeepEqual(store.applied, want) {
		t.Fatalf("applied = %v, want %v", store.applied, want)
	}
}

func TestSyncPushLabelsEachProjectWithItsOwnKit(t *testing.T) {
	svc, store, remote, _ := newSyncFixture(syncProject(1, "a"), syncProject(2, "b"))
	store.queued = map[int64][]domain.SyncChange{1: hlcChanges(1), 2: hlcChanges(1)}
	if _, err := svc.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(remote.pushed) != 2 || remote.pushed[0].KitKey != "omakase" || remote.pushed[1].KitKey != "lean" {
		t.Fatalf("pushed = %v, want kits omakase then lean", remote.pushed)
	}
}

func TestCloneRefusesDifferentKit(t *testing.T) {
	svc, store, remote, _ := newSyncFixture()
	info := domain.SyncProjectInfo{UID: domain.NewUID(), Slug: "r", KitKey: "other"}
	remote.projects = []domain.SyncProjectInfo{info}
	_, err := svc.Clone(context.Background(), info.UID, t.TempDir())
	assertCodedError(t, err, domain.ErrValidation)
	if !strings.Contains(err.Error(), "share the workflow kit") || len(store.cloned) != 0 {
		t.Fatalf("err = %v, cloned = %v", err, store.cloned)
	}
}

func TestCloneUnknownUID(t *testing.T) {
	svc, store, _, _ := newSyncFixture()
	_, err := svc.Clone(context.Background(), domain.NewUID(), t.TempDir())
	assertCodedError(t, err, domain.ErrProjectNotFound)
	if len(store.cloned) != 0 {
		t.Fatalf("cloned = %v", store.cloned)
	}
}

func TestSetRemoteValidation(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"http://localhost:8080", true},
		{"https://sync.example.com/base", true},
		{"", false},
		{"sync.example.com", false},
		{"ftp://host", false},
		{"http://", false},
		{"http://%zz", false},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			svc, store, _, _ := newSyncFixture()
			err := svc.SetRemote(context.Background(), tc.url)
			if tc.ok {
				if err != nil || store.url != tc.url {
					t.Fatalf("err = %v, stored = %q", err, store.url)
				}
				return
			}
			assertCodedError(t, err, domain.ErrValidation)
			if store.url != "" {
				t.Fatalf("stored %q despite error", store.url)
			}
		})
	}
}
