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

// MCPCommandBinding mirrors config.MCPCommandSpec on the agent side. The
// reserved name "global" supplies laws inherited by every command; per-command
// entries can add laws or opt out of inherited ones via LawsDisabled.
type MCPCommandBinding struct {
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

type CommandCatalog func() map[string]MCPCommandBinding

// ResolveCommandInput identifies the prompt to resolve. The agent service
// trims and normalizes the name, then walks the bundle bindings to assemble
// the persona/skills/laws/templates package the MCP layer renders into a
// single prompt message.
type ResolveCommandInput struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CommandListEntry is one row of the commands.list discovery surface:
// slug plus the entity-sourced playbook one-liner. Argument schemas
// stay in the MCP adapter (promptArguments) so the operation layer
// does not import protocol types.
type CommandListEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ListCommandsResponse is the gated catalog behind census slug
// command.list, distinct from command.resolve (one prompt package).
type ListCommandsResponse struct {
	Commands []CommandListEntry `json:"commands"`
}

// ResolveCommandResponse is the resolved package for one MCP prompt call.
// Markdown holds the single-message rendering the MCP layer ships to the
// agent; the structured fields are kept so callers can render the same data
// differently (logs, tests, alternate adapters).
type ResolveCommandResponse struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Persona     *PersonaInfo   `json:"persona,omitempty"`
	Skills      []SkillInfo    `json:"skills,omitempty"`
	Laws        []LawInfo      `json:"laws,omitempty"`
	Templates   []TemplateInfo `json:"templates,omitempty"`
	// InvocationArgs carries prompt invocation arguments from MCP prompts/get.
	// They render into the prompt body only when present, so command playbooks
	// that refer to "the task id" or "the slug" receive the concrete values the
	// user supplied without every playbook re-declaring an argument section.
	InvocationArgs []InvocationArg `json:"invocation_args,omitempty"`
	Markdown       string          `json:"markdown"`
	// AgentOutputLanguage carries the raw configured agent-output
	// language string (config.languages.agent_output). When non-empty,
	// renderCommandMarkdown appends a trailing "**Output language:** X"
	// line so the agent honors it for commits, docs, code comments,
	// and PR bodies. Empty means no directive is appended.
	AgentOutputLanguage string `json:"agent_output_language,omitempty"`
}

type InvocationArg struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
