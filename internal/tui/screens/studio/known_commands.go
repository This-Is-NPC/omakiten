package studio

// defaultKnownCommands is the ordered okt-* list the Commands pane treats as
// registered when Deps.CommandNames is empty (fixtures / tests). Production
// Bind always passes operation.CommandNames() from the host so this copy is
// a fallback, not a second owner.
var defaultKnownCommands = []string{
	"okt",
	"okt-help",
	"okt-start",
	"okt-shape",
	"okt-run",
	"okt-task-imagine",
	"okt-task-research",
	"okt-task-validate",
	"okt-task-requirements",
	"okt-task-prioritize",
	"okt-task-create",
	"okt-task-decompose",
	"okt-task-estimate",
	"okt-task-design",
	"okt-project-resume",
	"okt-project-continue",
	"okt-plan-create",
	"okt-plan-show",
	"okt-plan-continue",
	"okt-plan-claim",
	"okt-task-resume",
	"okt-task-continue",
	"okt-task-implement",
	"okt-task-self-review",
	"okt-task-refactor",
	"okt-task-document",
	"okt-task-debrief",
	"okt-config",
	"okt-skill",
	"okt-task-commit",
	"okt-task-review",
	"okt-task-secure",
	"okt-task-check",
	"okt-task-quality",
	"okt-audit",
	"okt-pause",
	"okt-note-free",
	"okt-note-recap",
	"okt-note-list",
	"okt-note-show"}

func (m Screen) knownCommandNames() []string {
	if len(m.repos.CommandNames) > 0 {
		return m.repos.CommandNames
	}
	return defaultKnownCommands
}
