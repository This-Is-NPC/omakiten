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
	rejects  int
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

func (f *fakeSyncStore) ApplyChanges(_ context.Context, _ int64, changes []domain.SyncChange, cursor int64) (int, error) {
	f.applied = append(f.applied, appliedPage{len(changes), cursor})
	return f.rejects, nil
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
	// calls counts KitKeyOf lookups per project; failing lists projects whose lookup errors.
	calls   map[int64]int
	failing map[int64]bool
}

func (k fakeSyncKits) KitKeyOf(_ context.Context, id int64) (string, error) {
	k.calls[id]++
	if k.failing[id] {
		return "", errors.New("kit lookup failed")
	}
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
	// pullErrs fails pulls for the listed project uids only.
	pullErrs map[domain.UID]error
	// pushErr, when set, decides whether a push of n changes fails.
	pushErr func(n int) error
	// pushRejected is how many changes each push reports as rejected.
	pushRejected int
	// pullLimits records the limit of every pull; pullErr fn, when set, decides whether a pull of that limit fails.
	pullLimits  []int
	pullLimitFn func(limit int) error
}

func (r *fakeSyncRemote) ListProjects(context.Context) ([]domain.SyncProjectInfo, error) {
	return r.projects, nil
}

func (r *fakeSyncRemote) Push(_ context.Context, info domain.SyncProjectInfo, changes []domain.SyncChange) (int64, int, error) {
	r.pushed = append(r.pushed, info)
	*r.log = append(*r.log, "push")
	r.pushes = append(r.pushes, len(changes))
	if r.pushErr != nil {
		if err := r.pushErr(len(changes)); err != nil {
			return 0, 0, err
		}
	}
	return 1, r.pushRejected, nil
}

func (r *fakeSyncRemote) Pull(_ context.Context, uid domain.UID, after int64, limit int) ([]domain.SyncChange, int64, bool, error) {
	*r.log = append(*r.log, "pull")
	r.pullLimits = append(r.pullLimits, limit)
	if r.pullLimitFn != nil {
		if err := r.pullLimitFn(limit); err != nil {
			return nil, 0, false, err
		}
	}
	r.pullsAt = append(r.pullsAt, after)
	if r.pullErr != nil {
		return nil, 0, false, r.pullErr
	}
	if err := r.pullErrs[uid]; err != nil {
		return nil, 0, false, err
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
	kits := fakeSyncKits{byProject: map[int64]string{1: "omakase", 2: "lean"}, atRoot: "omakase", calls: map[int64]int{}, failing: map[int64]bool{}}
	return NewSyncService(store, remote, kits), store, remote, log
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

func TestSyncReportsRejectedChanges(t *testing.T) {
	svc, store, remote, _ := newSyncFixture(domain.SyncProject{ID: 1, Info: domain.SyncProjectInfo{Slug: "a"}})
	store.rejects = 2
	remote.pages = []pullPage{{n: 3, cursor: 6, more: true}, {n: 1, cursor: 7}}
	report, err := svc.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Projects[0]; got.Pulled != 4 || got.Rejected != 4 {
		t.Fatalf("report = %+v, want 4 pulled and 4 rejected", got)
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
			err := svc.SetRemote(context.Background(), tc.url, false)
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

func tooLarge() error { return domain.NewError(domain.ErrPayloadTooLarge, "body too large", nil) }

func TestSyncProjectFailureDoesNotStopOthers(t *testing.T) {
	a, b := syncProject(1, "a"), syncProject(2, "b")
	svc, _, remote, _ := newSyncFixture(a, b)
	remote.pullErrs = map[domain.UID]error{a.Info.UID: errors.New("boom")}
	report, err := svc.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("partial failure returned %v", err)
	}
	if len(report.Projects) != 2 || !strings.Contains(report.Projects[0].Error, "boom") || report.Projects[1].Error != "" {
		t.Fatalf("report = %+v", report.Projects)
	}
	assertCodedError(t, report.Failure(), domain.ErrSyncFailed)
}

func TestSyncKitFailureIsRecordedPerProject(t *testing.T) {
	svc, _, _, _ := newSyncFixture(syncProject(1, "a"), syncProject(2, "b"))
	svc.kits.(fakeSyncKits).failing[1] = true
	report, err := svc.Sync(context.Background(), nil)
	if err != nil || !strings.Contains(report.Projects[0].Error, "kit") || report.Projects[1].Error != "" {
		t.Fatalf("report = %+v, err = %v", report.Projects, err)
	}
}

func TestSyncFailsWhenEveryProjectFailed(t *testing.T) {
	svc, _, remote, _ := newSyncFixture(syncProject(1, "a"), syncProject(2, "b"))
	remote.pullErr = errors.New("boom")
	report, err := svc.Sync(context.Background(), nil)
	assertCodedError(t, err, domain.ErrSyncFailed)
	if len(report.Projects) != 2 {
		t.Fatalf("report = %+v", report.Projects)
	}
}

func TestSyncResolvesKitOncePerProject(t *testing.T) {
	svc, store, _, _ := newSyncFixture(syncProject(1, "a"), syncProject(2, "b"))
	store.queued = map[int64][]domain.SyncChange{1: hlcChanges(syncPushPage + 1)}
	if _, err := svc.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if calls := svc.kits.(fakeSyncKits).calls; calls[1] != 1 || calls[2] != 1 {
		t.Fatalf("kit lookups = %v", calls)
	}
}

func TestSyncPullRefusesStalledAndBackwardCursor(t *testing.T) {
	cases := map[string][]pullPage{
		"more without progress":                {{n: 0, cursor: 4, more: true}},
		"more with rows but no cursor advance": {{n: 2, cursor: 4, more: true}},
		"cursor goes backwards":                {{n: 1, cursor: 3}},
	}
	for name, pages := range cases {
		t.Run(name, func(t *testing.T) {
			svc, store, remote, _ := newSyncFixture(domain.SyncProject{ID: 1, PullCursor: 4, Info: domain.SyncProjectInfo{Slug: "a"}})
			remote.pages = pages
			report, err := svc.Sync(context.Background(), nil)
			assertCodedError(t, err, domain.ErrSyncFailed)
			if !strings.Contains(report.Projects[0].Error, "no pull progress") || len(store.applied) != 0 {
				t.Fatalf("report = %+v, applied = %v", report.Projects, store.applied)
			}
		})
	}
}

func TestSyncReportsServerRejectedPushes(t *testing.T) {
	svc, store, remote, _ := newSyncFixture(syncProject(1, "a"))
	store.pending = hlcChanges(3)
	store.rejects = 1
	remote.pushRejected = 2
	remote.pages = []pullPage{{n: 1, cursor: 2}}
	report, err := svc.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Projects[0]; got.Pushed != 3 || got.Rejected != 3 {
		t.Fatalf("report = %+v, want 3 rejected (2 by server, 1 by store)", got)
	}
}

func TestSyncPullHalvesPageOnResponseTooLarge(t *testing.T) {
	svc, _, remote, _ := newSyncFixture(syncProject(1, "a"))
	remote.pullLimitFn = func(limit int) error {
		if limit > 100 {
			return tooLarge()
		}
		return nil
	}
	remote.pages = []pullPage{{n: 1, cursor: 2}}
	report, err := svc.Sync(context.Background(), nil)
	if err != nil || report.Projects[0].Error != "" || report.Projects[0].Pulled != 1 {
		t.Fatalf("report = %+v, err = %v", report.Projects, err)
	}
	if want := []int{500, 250, 125, 62}; !reflect.DeepEqual(remote.pullLimits, want) {
		t.Fatalf("limits = %v, want %v", remote.pullLimits, want)
	}
}

func TestSyncPullReportsSingleOversizedRow(t *testing.T) {
	svc, _, remote, _ := newSyncFixture(domain.SyncProject{ID: 1, PullCursor: 7, Info: domain.SyncProjectInfo{Slug: "a"}})
	remote.pullLimitFn = func(int) error { return tooLarge() }
	report, err := svc.Sync(context.Background(), nil)
	assertCodedError(t, err, domain.ErrSyncFailed)
	if !strings.Contains(report.Projects[0].Error, "cursor 7") || len(remote.pullLimits) != 9 {
		t.Fatalf("report = %+v, limits = %v, want 9 pulls (500 halved to 1)", report.Projects, remote.pullLimits)
	}
}

func TestSyncPullStopsAtPageCap(t *testing.T) {
	svc, _, remote, _ := newSyncFixture(syncProject(1, "a"))
	for i := range syncMaxPullPages + 1 {
		remote.pages = append(remote.pages, pullPage{n: 1, cursor: int64(i + 1), more: true})
	}
	report, err := svc.Sync(context.Background(), nil)
	assertCodedError(t, err, domain.ErrSyncFailed)
	if !strings.Contains(report.Projects[0].Error, "pull pages") || len(remote.pullsAt) != syncMaxPullPages {
		t.Fatalf("error = %q, pulls = %d", report.Projects[0].Error, len(remote.pullsAt))
	}
}

func TestSyncPushHalvesPageOnBodyTooLarge(t *testing.T) {
	svc, store, remote, _ := newSyncFixture(syncProject(1, "a"))
	store.pending = hlcChanges(10)
	remote.pushErr = func(n int) error {
		if n > 3 {
			return tooLarge()
		}
		return nil
	}
	report, err := svc.Sync(context.Background(), nil)
	if err != nil || report.Projects[0].Error != "" || report.Projects[0].Pushed != 10 {
		t.Fatalf("report = %+v, err = %v", report.Projects, err)
	}
	if want := []string{"h0002", "h0005", "h0008", "h0009"}; !reflect.DeepEqual(store.pushed, want) {
		t.Fatalf("marked = %v, want %v", store.pushed, want)
	}
}

func TestSyncPushReportsSingleOversizedChange(t *testing.T) {
	svc, store, remote, _ := newSyncFixture(syncProject(1, "a"))
	store.pending = []domain.SyncChange{{Table: "tasks", RowUID: "row-1", HLC: "h1"}}
	remote.pushErr = func(int) error { return tooLarge() }
	report, err := svc.Sync(context.Background(), nil)
	assertCodedError(t, err, domain.ErrSyncFailed)
	if !strings.Contains(report.Projects[0].Error, "tasks row-1") || len(remote.pushes) != 9 {
		t.Fatalf("report = %+v, pushes = %d, want 9 (500 halved to 1)", report.Projects, len(remote.pushes))
	}
}

func TestCloneTwiceRefusedWithSyncHint(t *testing.T) {
	info := domain.SyncProjectInfo{UID: domain.NewUID(), Slug: "r"}
	svc, store, remote, _ := newSyncFixture(domain.SyncProject{ID: 5, Info: info})
	remote.projects = []domain.SyncProjectInfo{info}
	_, err := svc.Clone(context.Background(), info.UID, t.TempDir())
	assertCodedError(t, err, domain.ErrValidation)
	if !strings.Contains(err.Error(), "okt sync --project r") || len(store.cloned) != 0 {
		t.Fatalf("err = %v, cloned = %v", err, store.cloned)
	}
}

func TestClonePullFailureTellsToSync(t *testing.T) {
	svc, _, remote, _ := newSyncFixture()
	info := domain.SyncProjectInfo{UID: domain.NewUID(), Slug: "r"}
	remote.projects = []domain.SyncProjectInfo{info}
	remote.pullErr = errors.New("boom")
	_, err := svc.Clone(context.Background(), info.UID, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "okt sync --project r") || !errors.Is(err, remote.pullErr) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetRemoteInsecure(t *testing.T) {
	svc, store, _, _ := newSyncFixture()
	err := svc.SetRemote(context.Background(), "http://server:8787", false)
	assertCodedError(t, err, domain.ErrValidation)
	for _, want := range []string{"https", "OMAKITEN_SYNC_INSECURE"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want mention of %s", err, want)
		}
	}
	if err := svc.SetRemote(context.Background(), "http://server:8787", true); err != nil || store.url == "" {
		t.Fatalf("err = %v, stored = %q", err, store.url)
	}
}
