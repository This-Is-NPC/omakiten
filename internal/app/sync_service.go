package app

import (
	"context"
	"errors"
	"fmt"

	"omakiten/internal/domain"
)

const (
	// syncPushPage is the most changes sent in one push request.
	syncPushPage = 500
	// syncPullPage is the most changes requested in one pull request.
	syncPullPage = 500
)

// SyncService exchanges changes of synced projects with the remote server.
type SyncService struct {
	store  SyncStore
	remote SyncRemote
	kits   SyncKits
}

// NewSyncService wires the sync flows. remote is nil when no server URL is
// configured; the flows that need it then fail with a validation error.
// kits labels each pushed project with its workflow kit and checks the kit of a
// project being cloned.
func NewSyncService(store SyncStore, remote SyncRemote, kits SyncKits) *SyncService {
	return &SyncService{store: store, remote: remote, kits: kits}
}

func (s *SyncService) requireRemote() error {
	if s.remote == nil {
		return domain.NewError(domain.ErrValidation,
			"no sync remote configured; run `okt remote set <url>`", nil)
	}
	return nil
}

// Sync pushes then pulls every synced project, or only projectID when set.
func (s *SyncService) Sync(ctx context.Context, projectID *int64) (domain.SyncReport, error) {
	if err := s.requireRemote(); err != nil {
		return domain.SyncReport{}, err
	}
	projects, err := s.store.SyncedProjects(ctx)
	if err != nil {
		return domain.SyncReport{}, err
	}
	if projectID != nil {
		projects, err = selectSyncProject(projects, *projectID)
		if err != nil {
			return domain.SyncReport{}, err
		}
	}
	report := domain.SyncReport{Projects: make([]domain.SyncProjectReport, 0, len(projects))}
	for _, p := range projects {
		report.Projects = append(report.Projects, s.syncProject(ctx, p))
	}
	if failure := report.Failure(); failure != nil && allFailed(report) {
		return report, failure
	}
	return report, nil
}

func allFailed(report domain.SyncReport) bool {
	for _, p := range report.Projects {
		if p.Error == "" {
			return false
		}
	}
	return true
}

// syncProject pushes then pulls one project; a failure lands in the returned
// report so the other projects still sync.
func (s *SyncService) syncProject(ctx context.Context, p domain.SyncProject) domain.SyncProjectReport {
	out := domain.SyncProjectReport{Slug: p.Info.Slug}
	kit, err := s.kits.KitKeyOf(ctx, p.ID)
	if err != nil {
		out.Error = fmt.Sprintf("kit: %v", err)
		return out
	}
	info := p.Info
	info.KitKey = kit
	if out.Pushed, err = s.push(ctx, p.ID, info); err != nil {
		out.Error = fmt.Sprintf("push: %v", err)
		return out
	}
	if out.Pulled, out.Rejected, err = s.pull(ctx, p.ID, p.Info.UID, p.PullCursor); err != nil {
		out.Error = fmt.Sprintf("pull: %v", err)
	}
	return out
}

func selectSyncProject(projects []domain.SyncProject, id int64) ([]domain.SyncProject, error) {
	for _, p := range projects {
		if p.ID == id {
			return []domain.SyncProject{p}, nil
		}
	}
	return nil, domain.NewError(domain.ErrProjectNotFound,
		fmt.Sprintf("project %d is not a synced project", id), map[string]any{"project_id": id})
}

// push sends pending changes page by page and advances the push watermark.
// A page the server refuses as too large is halved down to one change.
func (s *SyncService) push(ctx context.Context, projectID int64, info domain.SyncProjectInfo) (int, error) {
	total, page := 0, syncPushPage
	for {
		changes, err := s.store.PendingChanges(ctx, projectID, page)
		if err != nil {
			return total, err
		}
		if len(changes) == 0 {
			return total, nil
		}
		if _, err := s.remote.Push(ctx, info, changes); err != nil {
			if page, err = shrinkPushPage(page, changes[0], err); err != nil {
				return total, err
			}
			continue
		}
		if err := s.store.MarkPushed(ctx, projectID, changes[len(changes)-1].HLC); err != nil {
			return total, err
		}
		total += len(changes)
		if len(changes) < page {
			return total, nil
		}
	}
}

// shrinkPushPage halves page after the server refused a push as too large. Any
// other error, or a single change that is still too large, is returned.
func shrinkPushPage(page int, first domain.SyncChange, err error) (int, error) {
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrPayloadTooLarge {
		return page, err
	}
	if page == 1 {
		return page, fmt.Errorf("change %s %s is too large for the server: %w", first.Table, first.RowUID, err)
	}
	return page / 2, nil
}

// pull applies remote changes page by page from cursor until none follow and
// returns how many it pulled and how many the store rejected. A page that
// signals more without progress, or a cursor that goes backwards, fails
// instead of looping.
func (s *SyncService) pull(ctx context.Context, projectID int64, uid domain.UID, cursor int64) (total, rejected int, err error) {
	for {
		changes, next, more, err := s.remote.Pull(ctx, uid, cursor, syncPullPage)
		if err != nil {
			return total, rejected, err
		}
		if next < cursor || (more && len(changes) == 0 && next == cursor) {
			return total, rejected, domain.NewError(domain.ErrValidation,
				fmt.Sprintf("sync server made no pull progress (cursor %d, answered %d, more=%t)", cursor, next, more),
				map[string]any{"cursor": cursor, "next": next})
		}
		if len(changes) > 0 || next != cursor {
			n, err := s.store.ApplyChanges(ctx, projectID, changes, next)
			if err != nil {
				return total, rejected, err
			}
			rejected += n
		}
		total += len(changes)
		cursor = next
		if !more {
			return total, rejected, nil
		}
	}
}

// Clone creates a synced local project for a remote project and pulls its rows.
func (s *SyncService) Clone(ctx context.Context, uid domain.UID, root string) (domain.Project, error) {
	if err := s.requireRemote(); err != nil {
		return domain.Project{}, err
	}
	root, err := ValidateProjectRoot(root)
	if err != nil {
		return domain.Project{}, err
	}
	infos, err := s.remote.ListProjects(ctx)
	if err != nil {
		return domain.Project{}, err
	}
	for _, info := range infos {
		if info.UID != uid {
			continue
		}
		if err := s.checkKit(ctx, info, root); err != nil {
			return domain.Project{}, err
		}
		if err := s.refuseClonedTwice(ctx, info); err != nil {
			return domain.Project{}, err
		}
		project, err := s.store.CreateClonedProject(ctx, info, root)
		if err != nil {
			return domain.Project{}, err
		}
		if _, _, err := s.pull(ctx, project.ID, uid, 0); err != nil {
			return project, fmt.Errorf("clone %s: pull: %w; the project exists locally, run `okt sync --project %s` to finish", info.Slug, err, project.Slug)
		}
		return project, nil
	}
	return domain.Project{}, domain.NewError(domain.ErrProjectNotFound,
		fmt.Sprintf("remote has no project %s", uid), map[string]any{"uid": string(uid)})
}

// refuseClonedTwice points a repeated clone at okt sync instead of creating a second project.
func (s *SyncService) refuseClonedTwice(ctx context.Context, info domain.SyncProjectInfo) error {
	projects, err := s.store.SyncedProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if p.Info.UID == info.UID {
			return domain.NewError(domain.ErrValidation,
				fmt.Sprintf("project %s is already cloned as %q; run `okt sync --project %s` to update it", info.UID, p.Info.Slug, p.Info.Slug),
				map[string]any{"uid": string(info.UID), "slug": p.Info.Slug})
		}
	}
	return nil
}

// checkKit refuses a remote project whose workflow kit differs from the kit a project rooted at root would use.
func (s *SyncService) checkKit(ctx context.Context, info domain.SyncProjectInfo, root string) error {
	local, err := s.kits.KitKeyAt(ctx, root)
	if err != nil {
		return err
	}
	if info.KitKey == "" || info.KitKey == local {
		return nil
	}
	return domain.NewError(domain.ErrValidation,
		fmt.Sprintf("project %s uses workflow kit %q but a project at %s would use %q; both machines must share the workflow kit", info.Slug, info.KitKey, root, local),
		map[string]any{"remote_kit": info.KitKey, "local_kit": local})
}

// RemoteProjects lists the projects the remote server holds.
func (s *SyncService) RemoteProjects(ctx context.Context) ([]domain.SyncProjectInfo, error) {
	if err := s.requireRemote(); err != nil {
		return nil, err
	}
	return s.remote.ListProjects(ctx)
}

// Remote returns the configured server URL.
func (s *SyncService) Remote(ctx context.Context) (string, error) {
	return s.store.SyncRemoteURL(ctx)
}

// SetRemote validates and stores the server URL; insecure allows plain http to a non-loopback host.
func (s *SyncService) SetRemote(ctx context.Context, rawURL string, insecure bool) error {
	if err := domain.ValidateSyncRemoteURL(rawURL, insecure); err != nil {
		return err
	}
	return s.store.SetSyncRemoteURL(ctx, rawURL)
}
