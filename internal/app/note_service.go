package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// NoteService is the application-layer entry for the project-or-global
// notes entity (#360, umbrella #359). The service owns validation
// (non-empty title/body, soft 64 KiB body cap, scope resolution, tag
// normalisation) and delegates persistence to NoteRepository. The
// snapshot pointer powers the per-project tag synonym table reuse that
// CommentService also relies on.
type NoteService struct {
	repo NoteRepository
	snap *config.Snapshot
}

// NoteBodySoftLimit is the per-note body cap surfaced on the service
// boundary. Matches the storage layer constant; promoted here so MCP
// validation errors stay consistent with the limit the user reads in
// the docs.
const NoteBodySoftLimit = 64 * 1024

func NewNoteService(repo NoteRepository, snap *config.Snapshot) *NoteService {
	return &NoteService{repo: repo, snap: snap}
}

// CreateInput collects the fields NoteService.Create needs. Scope is a
// caller-supplied string ("global" or "project"); the service resolves
// it against the resolved project context so an empty scope on a
// project-resolved call defaults to project-scope. authorModel flows
// from ctx via activity.AgentModelFromContext when the caller did not
// supply one explicitly.
type CreateNoteInput struct {
	Scope       string
	Project     domain.ProjectContext
	Kind        string
	Title       string
	Body        string
	Pinned      bool
	AuthorModel string
	Tags        []string
}

func (s *NoteService) Create(ctx context.Context, input CreateNoteInput) (note domain.Note, err error) {
	finish := activity.Track(ctx, "app.NoteService.Create", input.Project, map[string]any{"scope": input.Scope, "kind": input.Kind})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	title := strings.TrimSpace(input.Title)
	body := strings.TrimSpace(input.Body)
	kind := strings.TrimSpace(input.Kind)
	if title == "" {
		err = domain.NewError(domain.ErrValidation, "note title is required", nil)
		return
	}
	if body == "" {
		err = domain.NewError(domain.ErrValidation, "note body is required", nil)
		return
	}
	if len(body) > NoteBodySoftLimit {
		err = domain.NewError(domain.ErrValidation, "note body exceeds soft limit", map[string]any{"limit_bytes": NoteBodySoftLimit, "size_bytes": len(body)})
		return
	}
	if kind == "" {
		err = domain.NewError(domain.ErrValidation, "note kind is required", nil)
		return
	}

	projectID, err := resolveNoteScope(input.Scope, input.Project)
	if err != nil {
		return
	}

	tags := s.normalizeTags(input.Tags)
	authorModel := strings.TrimSpace(input.AuthorModel)
	if authorModel == "" {
		_, _, modelFromCtx, _, _ := activity.FromContext(ctx)
		authorModel = modelFromCtx
	}

	note, err = s.repo.CreateNote(ctx, projectID, kind, title, body, input.Pinned, authorModel, tags)
	return
}

// EditNoteInput patches a note. Pointer fields mark intent: nil means
// "leave alone", non-nil means "overwrite". Tags pointer mirrors the
// repository semantics: nil = leave alone, non-nil empty slice = clear
// every tag.
type EditNoteInput struct {
	NoteID int64
	Title  *string
	Body   *string
	Kind   *string
	Pinned *bool
	Tags   *[]string
}

func (s *NoteService) Edit(ctx context.Context, input EditNoteInput) (note domain.Note, err error) {
	finish := activity.Track(ctx, "app.NoteService.Edit", domain.ProjectContext{}, map[string]any{"note_id": input.NoteID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if input.NoteID <= 0 {
		err = domain.NewError(domain.ErrValidation, "note id must be positive", nil)
		return
	}

	update := domain.NoteUpdate{}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			err = domain.NewError(domain.ErrValidation, "note title cannot be empty", nil)
			return
		}
		update.Title = &title
	}
	if input.Body != nil {
		body := strings.TrimSpace(*input.Body)
		if body == "" {
			err = domain.NewError(domain.ErrValidation, "note body cannot be empty", nil)
			return
		}
		if len(body) > NoteBodySoftLimit {
			err = domain.NewError(domain.ErrValidation, "note body exceeds soft limit", map[string]any{"limit_bytes": NoteBodySoftLimit, "size_bytes": len(body)})
			return
		}
		update.Body = &body
	}
	if input.Kind != nil {
		kind := strings.TrimSpace(*input.Kind)
		if kind == "" {
			err = domain.NewError(domain.ErrValidation, "note kind cannot be empty", nil)
			return
		}
		update.Kind = &kind
	}
	if input.Pinned != nil {
		update.Pinned = input.Pinned
	}
	if input.Tags != nil {
		normalized := make([]string, 0, len(*input.Tags))
		for _, raw := range *input.Tags {
			name := NormalizeTagName(raw, s.snap.Synonyms())
			if name == "" {
				continue
			}
			normalized = append(normalized, name)
		}
		update.Tags = &normalized
	}

	note, err = s.repo.UpdateNote(ctx, input.NoteID, update)
	return
}

// Show loads a single note + its tags.
func (s *NoteService) Show(ctx context.Context, id int64) (domain.Note, error) {
	if id <= 0 {
		return domain.Note{}, domain.NewError(domain.ErrValidation, "note id must be positive", nil)
	}
	return s.repo.NoteByID(ctx, id)
}

// ListNotesInput collects the filter options the MCP tool accepts.
// Scope defaults to "any" so a no-arg call returns every note in the
// resolved project plus every global note (project_id IS NULL).
type ListNotesInput struct {
	Scope     string
	Project   domain.ProjectContext
	Kind      string
	Tags      []string
	Pinned    *bool
	Limit     int
	Offset    int
}

func (s *NoteService) List(ctx context.Context, input ListNotesInput) ([]domain.Note, error) {
	scope, err := parseNoteScope(input.Scope)
	if err != nil {
		return nil, err
	}
	filter := domain.NoteFilter{
		Scope:     scope,
		ProjectID: input.Project.ID,
		Kind:      input.Kind,
		Pinned:    input.Pinned,
		Limit:     input.Limit,
		Offset:    input.Offset,
	}
	if scope == domain.NoteScopeProject && filter.ProjectID == 0 {
		return nil, domain.NewError(domain.ErrValidation, "scope=project requires a resolved project", nil)
	}
	if len(input.Tags) > 0 {
		seen := map[string]struct{}{}
		for _, raw := range input.Tags {
			name := NormalizeTagName(raw, s.snap.Synonyms())
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			filter.Tags = append(filter.Tags, name)
		}
	}
	return s.repo.ListNotes(ctx, filter)
}

// Delete enforces the explicit confirmation flag. Callers MUST pass
// confirm=true; the agent layer surfaces a Confirmation block when
// they forget, mirroring the comments.delete UX.
func (s *NoteService) Delete(ctx context.Context, id int64, confirm bool) error {
	if id <= 0 {
		return domain.NewError(domain.ErrValidation, "note id must be positive", nil)
	}
	if !confirm {
		return domain.NewError(domain.ErrValidation, "delete requires confirm=true", map[string]any{"note_id": id})
	}
	return s.repo.DeleteNote(ctx, id)
}

func (s *NoteService) normalizeTags(raw []string) []domain.Tag {
	if len(raw) == 0 {
		return nil
	}
	synonyms := s.snap.Synonyms()
	tags := make([]domain.Tag, 0, len(raw))
	for _, r := range raw {
		name := NormalizeTagName(r, synonyms)
		if name == "" {
			continue
		}
		tags = append(tags, domain.Tag{Name: name, Label: TagLabel(r)})
	}
	return tags
}

// resolveNoteScope maps the caller-supplied scope string into the
// numeric project_id the storage layer expects. "global" forces
// project_id = 0 even when the caller resolved a project (admin/
// global-note tooling); "project" requires a resolved project and uses
// its id; "" defaults to project-scope when a project was resolved,
// global otherwise.
func resolveNoteScope(scope string, project domain.ProjectContext) (int64, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "global":
		return 0, nil
	case "project":
		if project.ID == 0 {
			return 0, domain.NewError(domain.ErrValidation, "scope=project requires a resolved project", nil)
		}
		return project.ID, nil
	case "":
		if project.ID == 0 {
			return 0, nil
		}
		return project.ID, nil
	default:
		return 0, domain.NewError(domain.ErrValidation, "invalid scope", map[string]any{"scope": scope, "allowed": []string{"global", "project"}})
	}
}

func parseNoteScope(scope string) (domain.NoteScope, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "":
		return domain.NoteScopeAny, nil
	case "global":
		return domain.NoteScopeGlobal, nil
	case "project":
		return domain.NoteScopeProject, nil
	default:
		return domain.NoteScopeAny, domain.NewError(domain.ErrValidation, "invalid scope", map[string]any{"scope": scope, "allowed": []string{"global", "project"}})
	}
}
