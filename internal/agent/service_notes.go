package agent

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// resolveNoteProject mirrors resolveSearchProject: notes can be global
// (scope=global), so a missing project selector does NOT fall back to
// the agent's default cwd-resolved project — instead it returns a zero
// ProjectContext which the service layer treats as "no project". The
// scope string supplied by the caller drives the final project_id
// assignment in app.NoteService.
func (s *Service) resolveNoteProject(ctx context.Context, selector ProjectSelector, scope string) (domain.ProjectContext, error) {
	// Explicit project selectors always win.
	if selector.ProjectID > 0 {
		project, err := s.repo.FindProjectByID(ctx, selector.ProjectID)
		if err != nil {
			return domain.ProjectContext{}, err
		}
		return project.Context(), nil
	}
	if slug := strings.TrimSpace(selector.Project); slug != "" {
		project, err := s.repo.FindProjectBySlug(ctx, slug)
		if err != nil {
			return domain.ProjectContext{}, err
		}
		return project.Context(), nil
	}
	// scope=global: never reach for cwd; global notes belong nowhere.
	if strings.EqualFold(strings.TrimSpace(scope), "global") {
		return domain.ProjectContext{}, nil
	}
	// CWD or default: fall back to the agent's selector, swallowing
	// the "no project at cwd" error so global-only setups stay usable.
	project, err := s.resolveProject(ctx, selector)
	if err != nil {
		var coded *domain.CodedError
		if asCoded(err, &coded) && coded.Code == domain.ErrProjectNotFound {
			return domain.ProjectContext{}, nil
		}
		return domain.ProjectContext{}, err
	}
	return project, nil
}

func (s *Service) newNoteService() *app.NoteService {
	return app.NewNoteService(s.repo, s.snapshot)
}

func (s *Service) CreateNote(ctx context.Context, input CreateNoteInput) (NoteResponse, error) {
	project, err := s.resolveNoteProject(ctx, input.ProjectSelector, input.Scope)
	if err != nil {
		return NoteResponse{}, err
	}
	note, err := s.newNoteService().Create(ctx, app.CreateNoteInput{
		Scope:   input.Scope,
		Project: project,
		Kind:    input.Kind,
		Title:   input.Title,
		Body:    input.Body,
		Pinned:  input.Pinned,
		Tags:    input.Tags,
	})
	if err != nil {
		return NoteResponse{}, err
	}
	return NoteResponse{Project: projectSummary(project), Note: noteSummary(note)}, nil
}

func (s *Service) EditNote(ctx context.Context, input EditNoteInput) (NoteResponse, error) {
	// Project selector here is informational — the note id alone
	// determines the row. Resolve a project context for the response
	// envelope when one is available so the response shape stays
	// consistent with CreateNote / ShowNote.
	project, _ := s.resolveNoteProject(ctx, input.ProjectSelector, "")
	note, err := s.newNoteService().Edit(ctx, app.EditNoteInput{
		NoteID: input.ID,
		Title:  input.Title,
		Body:   input.Body,
		Kind:   input.Kind,
		Pinned: input.Pinned,
		Tags:   input.Tags,
	})
	if err != nil {
		return NoteResponse{}, err
	}
	return NoteResponse{Project: projectSummary(project), Note: noteSummary(note)}, nil
}

func (s *Service) ShowNote(ctx context.Context, input ShowNoteInput) (NoteResponse, error) {
	project, _ := s.resolveNoteProject(ctx, input.ProjectSelector, "")
	note, err := s.newNoteService().Show(ctx, input.ID)
	if err != nil {
		return NoteResponse{}, err
	}
	return NoteResponse{Project: projectSummary(project), Note: noteSummary(note)}, nil
}

func (s *Service) ListNotes(ctx context.Context, input ListNotesInput) (NotesResponse, error) {
	project, err := s.resolveNoteProject(ctx, input.ProjectSelector, input.Scope)
	if err != nil {
		return NotesResponse{}, err
	}
	notes, err := s.newNoteService().List(ctx, app.ListNotesInput{
		Scope:   input.Scope,
		Project: project,
		Kind:    input.Kind,
		Tags:    input.Tags,
		Pinned:  input.Pinned,
		Limit:   input.Limit,
		Offset:  input.Offset,
	})
	if err != nil {
		return NotesResponse{}, err
	}
	return NotesResponse{Project: projectSummary(project), Notes: noteSummaries(notes)}, nil
}

func (s *Service) DeleteNote(ctx context.Context, input DeleteNoteInput) (DeleteNoteResponse, error) {
	project, _ := s.resolveNoteProject(ctx, input.ProjectSelector, "")
	if !input.Confirm {
		return DeleteNoteResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Deleting a note is destructive. Confirm with confirm=true to proceed.",
				Options: []ConfirmationOption{
					{Action: "confirm_delete", Label: "Retry notes_delete with confirm=true to hard-delete"},
				},
			},
		}, nil
	}
	if err := s.newNoteService().Delete(ctx, input.ID, input.Confirm); err != nil {
		return DeleteNoteResponse{}, err
	}
	return DeleteNoteResponse{Project: projectSummary(project), Deleted: true}, nil
}

// asCoded mirrors domain.asCoded for the agent package — kept tiny so
// the agent layer does not have to import the unexported helper.
func asCoded(err error, target **domain.CodedError) bool {
	for err != nil {
		if c, ok := err.(*domain.CodedError); ok {
			*target = c
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
