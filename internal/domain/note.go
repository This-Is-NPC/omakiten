package domain

// Note is a project-or-global knowledge row introduced by umbrella #359
// (storage land in #360). Scope is encoded in ProjectID: 0 = global,
// non-zero = project-scoped. Kind is a free string (convention:
// "handoff", "decision", "architecture", "requirements", "runbook",
// "gotcha", "retrospective", "glossary", "free") — the storage layer
// does not enforce an enum so users can introduce new kinds without
// a migration.
type Note struct {
	ID          int64  `json:"id"`
	ProjectID   int64  `json:"project_id,omitempty"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Pinned      bool   `json:"pinned,omitempty"`
	AuthorModel string `json:"author_model,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Tags        []Tag  `json:"tags,omitempty"`
}

// NoteFilter narrows a notes.list query. Zero values degrade to "no
// filter" so the empty value means "all notes the caller can see".
// Scope is tri-state via the typed Scope enum: NoteScopeAny does not
// constrain project_id, NoteScopeGlobal forces project_id IS NULL,
// NoteScopeProject forces project_id = ProjectID.
type NoteFilter struct {
	Scope     NoteScope
	ProjectID int64
	Kind      string
	Tags      []string
	Pinned    *bool
	Limit     int
	Offset    int
}

// NoteScope encodes the project_id filter for notes.list. The zero
// value (NoteScopeAny) does not constrain project_id so cross-scope
// callers (cover sheet / admin views) can read every note in one call.
type NoteScope int

const (
	NoteScopeAny NoteScope = iota
	NoteScopeGlobal
	NoteScopeProject
)

// NoteUpdate patches a note. Pointer fields encode the "omitted vs
// explicit" distinction (nil = leave untouched, non-nil = overwrite).
// Tags carries the full replacement set when non-nil so callers can
// clear all tags by passing an empty slice (len(*Tags) == 0) instead
// of nil.
type NoteUpdate struct {
	Title  *string
	Body   *string
	Kind   *string
	Pinned *bool
	Tags   *[]string
}
