package agent

// PersonaInfo, SkillInfo, LawInfo, TemplateInfo are the agent-facing snapshots
// of bundle entities used by ResolveCommand. They mirror the relevant subset
// of internal/config types without importing it, so the agent layer stays
// protocol- and config-neutral.

type PersonaInfo struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Body        string   `json:"body,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Laws        []string `json:"laws,omitempty"`
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
	Name string `json:"name"`
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
	Action      string         `json:"action"`
	Markdown    string         `json:"markdown"`
	// AgentOutputLanguage carries the raw configured agent-output
	// language string (config.languages.agent_output). When non-empty,
	// renderCommandMarkdown appends a trailing "**Output language:** X"
	// line so the agent honors it for commits, docs, code comments,
	// and PR bodies. Empty means no directive is appended.
	AgentOutputLanguage string `json:"agent_output_language,omitempty"`
}

// MCPCommandsGlobalKey mirrors config.MCPCommandsGlobalKey on the agent side.
// Duplicating it here avoids a config import while keeping the contract
// stable: the runtime emits the same key.
const MCPCommandsGlobalKey = "global"
