package contract

type PersonaInfo struct {
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Body            string   `json:"body,omitempty"`
	SkillRepertoire []string `json:"skill_repertoire,omitempty"`
	Laws            []string `json:"laws,omitempty"`
}

type SkillInfo struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
}

type LawInfo struct {
	Slug     string `json:"slug"`
	Name     string `json:"name,omitempty"`
	Severity string `json:"severity"`
	Body     string `json:"body"`
	Scope    string `json:"scope,omitempty"`
	Project  string `json:"project,omitempty"`
	Persona  string `json:"persona,omitempty"`
}

type TemplateInfo struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Default     string   `json:"default,omitempty"`
	Project     string   `json:"project,omitempty"`
	Laws        []string `json:"laws,omitempty"`
	Body        string   `json:"body"`
}

// CommandBinding mirrors config.CommandSpec on the agent side. The
// reserved name "global" supplies laws inherited by every command; per-command
// entries can add laws or opt out of inherited ones via LawsDisabled.
type CommandBinding struct {
	Persona      string   `json:"persona,omitempty"`
	Laws         []string `json:"laws,omitempty"`
	LawsDisabled []string `json:"laws_disabled,omitempty"`
	Templates    []string `json:"templates,omitempty"`
	// Skills is the per-command skill selection — a minimal
	// subset of the bound persona's skill_repertoire. When set, it wins over
	// the persona's full repertoire so a themed command ships only the 2-4
	// skills relevant to its step. Empty uses the persona repertoire.
	Skills []string `json:"skills,omitempty"`
}

// SkillCatalog, LawCatalog, PersonaCatalog and CommandCatalog are the lookup
// closures the runtime injects so the agent service can resolve persona, law,
// skill and command bindings without importing the config package.
type SkillCatalog func() []SkillInfo

type LawCatalog func() []LawInfo

type PersonaCatalog func() []PersonaInfo

type CommandCatalog func() map[string]CommandBinding

// ResolveCommandInput identifies the prompt to resolve. The agent service
// trims and normalizes the name, then walks the bundle bindings to assemble
// the persona/skills/laws/templates package the agent layer renders into a
// single prompt message.
type ResolveCommandInput struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CommandListEntry identifies a playbook and its entity-sourced description.
type CommandListEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ListCommandsResponse is the gated catalog behind census slug
// command.list, distinct from command.resolve (one prompt package).
type ListCommandsResponse struct {
	Commands []CommandListEntry `json:"commands"`
}

// ResolveCommandResponse carries composed playbook context and its Markdown rendering.
type ResolveCommandResponse struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Persona     *PersonaInfo   `json:"persona,omitempty"`
	Skills      []SkillInfo    `json:"skills,omitempty"`
	Laws        []LawInfo      `json:"laws,omitempty"`
	Templates   []TemplateInfo `json:"templates,omitempty"`
	// InvocationArgs carries the concrete values supplied when resolving a playbook.
	InvocationArgs []InvocationArg `json:"invocation_args,omitempty"`
	Markdown       string          `json:"markdown"`
	// AgentOutputLanguage carries the raw configured agent-output
	// language string (application preferences.languages.agent_output). When non-empty,
	// renderCommandMarkdown appends a trailing "**Output language:** X"
	// line so the agent honors it for commits, docs, code comments,
	// and PR bodies. Empty means no directive is appended.
	AgentOutputLanguage string `json:"agent_output_language,omitempty"`
}

type InvocationArg struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
