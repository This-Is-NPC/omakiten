package app

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TUISnapshot is the coherent read-model the TUI renders from. It bundles
// every collection a refresh tick needs into one struct so the consumer
// (internal/tui/model.go's refresh) does not have to fan out to a half-
// dozen repositories itself.
type TUISnapshot struct {
	Tasks        []domain.Task
	Workflow     domain.Workflow
	Dependencies []domain.TaskDependency
	Comments     []domain.Comment
	Laws         []domain.Law
	Skills       []domain.Skill
	Personas     []domain.Persona
	Templates    []config.TaskTemplate
	AllTags      []domain.Tag
	TaskTagsByID map[int64][]domain.Tag
}

// TUIQueryService is the in-app aggregator that satisfies TUIQuery. It
// centralises the multi-repo read fan-out the TUI used to inline in its
// refresh method. Pulling it here keeps the TUI free of per-port plumbing
// and gives the read pipeline a single test seam. It is NOT a sqlite
// adapter — it composes Task/Dependency/Comment/Tag repository ports with
// the cached *config.Snapshot.
//
// Phase 3 perf note: the service deliberately does NOT hold a BundleEditor.
// Every entity slice the TUI renders (skills, laws, personas, templates,
// warnings) is sourced from the cached *config.Snapshot the runtime
// installed at boot — refresh is a hot path and a disk re-walk on each
// tick would pin a goroutine on os.ReadFile + YAML decode for ~50ms on a
// realistic bundle. Edits still rotate the cache via BundleCache.Reload,
// so a write-then-render cycle still sees the new bytes.
type TUIQueryService struct {
	tasks    TaskRepository
	snap     *config.Snapshot
	deps     DependencyRepository
	comments CommentRepository
	tags     TagRepository
}

// Compile-time check: the aggregator implements the read-model port.
var _ TUIQuery = (*TUIQueryService)(nil)

// NewTUIQueryService wires the TUI read-model aggregator and returns it as
// the TUIQuery port so composition roots / the TUI host depend on the
// interface, not the concrete type.
func NewTUIQueryService(tasks TaskRepository, snap *config.Snapshot, deps DependencyRepository, comments CommentRepository, tags TagRepository) TUIQuery {
	return &TUIQueryService{tasks: tasks, snap: snap, deps: deps, comments: comments, tags: tags}
}

// SnapshotOptions tunes the TUI snapshot fetch. IncludeArchived flips the
// active-only filter so the user can opt into seeing archived tasks via the
// `A` toggle in board/table/graph/logs.
type SnapshotOptions struct {
	IncludeArchived bool
}

// Snapshot loads every collection the TUI needs to render the board, the
// activity feeds and the entity views in one shot. Order: tasks first
// (most likely to fail with a fresh empty store), workflow, dependencies,
// comments, the entity collections (with on-disk enrichment when the
// editor is available), settings, then tag indexes.
func (s *TUIQueryService) Snapshot(ctx context.Context, project domain.ProjectContext, sort domain.TaskSort, opts ...SnapshotOptions) (TUISnapshot, error) {
	snap := TUISnapshot{TaskTagsByID: map[int64][]domain.Tag{}}

	var options SnapshotOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	tasks, err := s.tasks.ListTasks(ctx, project.ID, domain.TaskFilter{Sort: sort, IncludeArchived: options.IncludeArchived}, s.snap)
	if err != nil {
		return snap, err
	}
	snap.Tasks = tasks

	cfgSnap := s.snap
	if cfgSnap != nil {
		snap.Workflow = cfgSnap.Workflow()
	}

	deps, err := s.deps.ListTaskDependencies(ctx, project.ID, 0)
	if err != nil {
		return snap, err
	}
	snap.Dependencies = deps

	comments, err := s.comments.ListComments(ctx, project.ID, 0)
	if err != nil {
		return snap, err
	}
	snap.Comments = comments

	loadEntitySnapshot(&snap, cfgSnap)

	if err := s.loadTagSnapshot(ctx, project.ID, &snap); err != nil {
		return snap, err
	}

	return snap, nil
}

func loadEntitySnapshot(out *TUISnapshot, cfgSnap *config.Snapshot) {
	if cfgSnap == nil {
		return
	}
	// The snapshot carries entity bodies and warnings, so refresh does not scan disk.
	laws := allLawsFromSnapshot(cfgSnap)
	skills := allSkillsFromSnapshot(cfgSnap)
	personas := allPersonasFromSnapshot(cfgSnap)
	warnings := bundleWarningIndex(cfgSnap.Warnings())
	for i := range skills {
		if warning, ok := warnings[skills[i].Key]; ok {
			skills[i].Warning = warning
		}
	}
	for i := range laws {
		if warning, ok := warnings[laws[i].Key]; ok {
			laws[i].Warning = warning
		}
	}
	for i := range personas {
		if warning, ok := warnings[personas[i].Key]; ok {
			personas[i].Warning = warning
		}
	}
	out.Laws, out.Skills, out.Personas = laws, skills, personas
	out.Templates = append([]config.TaskTemplate(nil), cfgSnap.AllTemplates()...)
}

func (s *TUIQueryService) loadTagSnapshot(ctx context.Context, projectID int64, out *TUISnapshot) error {
	if s.tags == nil {
		return nil
	}
	allTags, err := s.tags.ListAllTags(ctx)
	if err != nil {
		return err
	}
	taskTags, err := s.tags.ListTaskTagsByProject(ctx, projectID)
	if err != nil {
		return err
	}
	out.AllTags, out.TaskTagsByID = allTags, taskTags
	return nil
}

// bundleWarningIndex returns the first source-warning message keyed by
// slug. Mirrors what the CLI's `list` paths report so the TUI surfaces the
// same non-fatal issues.
func bundleWarningIndex(warnings []config.SourceWarning) map[string]string {
	out := map[string]string{}
	for _, w := range warnings {
		if w.Slug == "" {
			continue
		}
		if _, exists := out[w.Slug]; exists {
			continue
		}
		out[w.Slug] = w.Message
	}
	return out
}
