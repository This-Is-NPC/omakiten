package agent

// PersonaInfo, SkillInfo, LawInfo, TemplateInfo are the agent-facing snapshots
// of bundle entities used by ResolveCommand. They mirror the relevant subset
// of internal/config types without importing it, so the agent layer stays
// protocol- and config-neutral.

type PersonaInfo struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	// Skills is the legacy v1 directly-wired skill list. SkillRepertoire
	// (schema v2) is the persona's full skill pool from which a command may
	// select a subset. A command that declares no command-level skills falls
	// back to Skills (v1), then SkillRepertoire (v2).
	Skills          []string `json:"skills,omitempty"`
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
	// Skills (schema v2) is the per-command skill selection — a minimal
	// subset of the bound persona's skill_repertoire. When set, it wins over
	// the persona's full repertoire so a themed command ships only the 2-4
	// skills relevant to its step. Empty falls back to the persona repertoire
	// for backward compatibility with presets that have not yet wired
	// command-level skills.
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

// MCPCommandsGlobalKey mirrors config.MCPCommandsGlobalKey on the agent side.
// Duplicating it here avoids a config import while keeping the contract
// stable: the runtime emits the same key.
const MCPCommandsGlobalKey = "global"
