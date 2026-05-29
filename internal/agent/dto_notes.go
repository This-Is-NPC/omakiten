package agent

import "omakiten/internal/domain"

// NoteSummary is the agent-facing shape of a note row. ProjectID is
// emitted as 0 for global notes — the omitempty tag means the field
// disappears from JSON when the note is global, matching the project
// = NULL semantics callers see in the storage layer.
type NoteSummary struct {
	ID          int64        `json:"id"`
	ProjectID   int64        `json:"project_id,omitempty"`
	Kind        string       `json:"kind"`
	Title       string       `json:"title"`
	Body        string       `json:"body"`
	Pinned      bool         `json:"pinned,omitempty"`
	AuthorModel string       `json:"author_model,omitempty"`
	CreatedAt   string       `json:"created_at,omitempty"`
	UpdatedAt   string       `json:"updated_at,omitempty"`
	Tags        []TagSummary `json:"tags,omitempty"`
}

// CreateNoteInput is the MCP shape for notes_create. Scope drives the
// project_id assignment: "project" uses the resolved project, "global"
// forces project_id IS NULL even on a project-scoped call (admin /
// cross-project tooling), and "" defaults to project when one is
// resolved, global otherwise.
type CreateNoteInput struct {
	ProjectSelector
	Scope  string   `json:"scope,omitempty"`
	Kind   string   `json:"kind"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Pinned bool     `json:"pinned,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

// EditNoteInput patches a note. Pointer-shaped optional fields use the
// `*string` / `*bool` idiom so the JSON omitted vs explicit-empty
// distinction reaches the service layer intact (empty string still
// errors as "cannot be empty"; omission leaves the field alone).
type EditNoteInput struct {
	ProjectSelector
	ID     int64     `json:"id"`
	Title  *string   `json:"title,omitempty"`
	Body   *string   `json:"body,omitempty"`
	Kind   *string   `json:"kind,omitempty"`
	Pinned *bool     `json:"pinned,omitempty"`
	Tags   *[]string `json:"tags,omitempty"`
}

type ShowNoteInput struct {
	ProjectSelector
	ID int64 `json:"id"`
}

type ListNotesInput struct {
	ProjectSelector
	Scope  string   `json:"scope,omitempty"`
	Kind   string   `json:"kind,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Pinned *bool    `json:"pinned,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Offset int      `json:"offset,omitempty"`
}

type DeleteNoteInput struct {
	ProjectSelector
	ID      int64 `json:"id"`
	Confirm bool  `json:"confirm,omitempty"`
}

type NoteResponse struct {
	Project ProjectSummary `json:"project"`
	Note    NoteSummary    `json:"note"`
}

type NotesResponse struct {
	Project ProjectSummary `json:"project"`
	Notes   []NoteSummary  `json:"notes"`
}

type DeleteNoteResponse struct {
	Project      ProjectSummary `json:"project"`
	Confirmation Confirmation   `json:"confirmation,omitempty"`
	Deleted      bool           `json:"deleted,omitempty"`
}

func noteSummary(note domain.Note) NoteSummary {
	s := NoteSummary{
		ID:          note.ID,
		ProjectID:   note.ProjectID,
		Kind:        note.Kind,
		Title:       note.Title,
		Body:        note.Body,
		Pinned:      note.Pinned,
		AuthorModel: note.AuthorModel,
		CreatedAt:   note.CreatedAt,
		UpdatedAt:   note.UpdatedAt,
	}
	if len(note.Tags) > 0 {
		s.Tags = tagSummaries(note.Tags)
	}
	return s
}

func noteSummaries(notes []domain.Note) []NoteSummary {
	out := make([]NoteSummary, 0, len(notes))
	for _, n := range notes {
		out = append(out, noteSummary(n))
	}
	return out
}
