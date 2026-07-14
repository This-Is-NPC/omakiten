package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode"

	"omakiten/internal/activity"
	"omakiten/internal/agent"
	"omakiten/internal/domain"
)

// agentModelArgKey / agentSessionArgKey are reserved top-level fields on
// every MCP tool input. _agent_model is required (coercive validation
// rejects calls missing it); _agent_session_id is optional. They are
// stripped from args before tool-specific decoding so individual input
// structs don't have to care.
//
// Both fields are also declared on every tool's InputSchema by
// withAgentAttribution so schema-aware clients (Claude Code harness,
// JSON-Schema-validating SDKs) include them automatically — without that
// declaration the client strips the unknown keys and the call fails with
// a "_agent_model is required" rejection the LLM cannot recover from.
const (
	agentModelArgKey   = "_agent_model"
	agentSessionArgKey = "_agent_session_id"
)

// maxAgentModelBytes caps the accepted _agent_model length. The value is
// stamped into events verbatim and later surfaces in GROUP BY rosters
// (metrics.summary, insights.summary per-model) and in the TUI, so an
// unbounded string would let a client mint arbitrarily large roster rows.
// 128 bytes comfortably fits every real model identifier.
const maxAgentModelBytes = 128

const agentModelSchemaDescription = "AI model identifier invoking this tool (e.g. \"claude-opus-4-7\", \"claude-sonnet-4-6\", \"gpt-5\"). Required — the server rejects calls without it so /metrics.summary can benchmark per-model behaviour."

const agentSessionSchemaDescription = "Optional session id correlating multiple tool calls from the same agent run. Stripped before tool-specific decoding."

// extractAgentAttribution pulls the reserved keys out of args, rejecting
// the call when _agent_model is absent or empty. Failing closed forces
// every benchmark sample to carry a model id, which is the whole point of
// /metrics.summary. The schema (see withAgentAttribution) carries the
// usage hint; this guard exists for clients that bypass schema validation.
func extractAgentAttribution(args map[string]any) (model, sessionID string, err error) {
	rawModel, ok := args[agentModelArgKey]
	if !ok {
		return "", "", domain.NewError(domain.ErrValidation,
			"_agent_model is required on all MCP tool calls (see tool inputSchema for usage).", nil)
	}
	model, _ = rawModel.(string)
	if model == "" {
		return "", "", domain.NewError(domain.ErrValidation,
			"_agent_model must be a non-empty string (see tool inputSchema for usage).", nil)
	}
	// Cap and charset-check at the single choke point every tool call
	// passes through: the value lands in events verbatim and is later
	// rendered in a terminal, so control/escape bytes are rejected rather
	// than stored. unicode.IsControl covers the full Cc category — C0
	// (<0x20), DEL (0x7f), AND the C1 block (0x80–0x9F) whose U+009B/U+009D
	// act as 8-bit CSI/OSC introducers on C1-honoring terminals; a
	// C0/DEL-only check would let those through.
	if len(model) > maxAgentModelBytes {
		return "", "", domain.NewError(domain.ErrValidation,
			"_agent_model exceeds 128 bytes; pass the plain model identifier.", nil)
	}
	for _, r := range model {
		if unicode.IsControl(r) {
			return "", "", domain.NewError(domain.ErrValidation,
				"_agent_model must not contain control characters.", nil)
		}
	}
	delete(args, agentModelArgKey)

	if raw, present := args[agentSessionArgKey]; present {
		sessionID, _ = raw.(string)
		delete(args, agentSessionArgKey)
	}
	return model, sessionID, nil
}

type Adapter struct {
	// service is the static fallback NewAdapter captured. It is only
	// consulted when defaultProvider is nil (test paths that never
	// touch a BundleCache). Production wires defaultProvider via
	// SetDefaultServiceProvider so cache rebuilds always surface the
	// fresh service — a stale *agent.Service pointer was the Phase
	// 3b regression the provider fixes.
	service         *agent.Service
	defaultProvider DefaultServiceProvider
	repo            activity.ActivityLogRepository
	resolver        ServiceResolver
}

// ServiceResolver returns the per-project agent.Service that should
// handle a tool call. Implementations consult the per-process
// BundleCache (see agentruntime) and return the cached project's
// service, falling back to the default when project is empty or
// unresolvable. Returning (nil, nil) means "use the adapter's default
// service" — the adapter treats it as an idiomatic no-op rather than
// an error so resolvers can keep their decision tree shallow.
type ServiceResolver func(ctx context.Context, project string, projectID int64) (*agent.Service, error)

// DefaultServiceProvider returns the adapter's fallback service on
// every call. Threaded through SetDefaultServiceProvider so the
// adapter never caches a *agent.Service pointer that could go stale
// after a BundleCache rebuild. Returning nil falls back to the static
// service NewAdapter captured.
type DefaultServiceProvider func() *agent.Service

func NewAdapter(service *agent.Service) *Adapter {
	return &Adapter{service: service}
}

// SetDefaultServiceProvider replaces the static default service with
// a function the adapter consults on every CallTool / ReadResource.
// Required for any runtime that mutates the default service after
// adapter construction (mtime-driven BundleCache rebuilds, explicit
// Reload). Pass nil to revert to the captured static service.
func (a *Adapter) SetDefaultServiceProvider(provider DefaultServiceProvider) {
	a.defaultProvider = provider
}

// defaultService resolves the adapter's fallback service through the
// provider when wired, falling back to the static service NewAdapter
// captured. Returns nil when neither source produced a service —
// CallTool surfaces that to the caller as a configuration error.
func (a *Adapter) defaultService() *agent.Service {
	if a.defaultProvider != nil {
		if svc := a.defaultProvider(); svc != nil {
			return svc
		}
	}
	return a.service
}

func (a *Adapter) SetActivityLogRepository(repo activity.ActivityLogRepository) {
	a.repo = repo
}

// SetServiceResolver installs the per-project routing function. When
// present, every CallTool peeks `project` / `project_id` from the
// incoming args and asks the resolver which agent.Service should
// handle the call. The default service is used when the resolver is
// absent, when it returns nil, or when the args do not declare a
// project — that mirrors the pre-3b single-project behaviour for the
// installed tests and for clients that have not yet adopted per-project
// args.
func (a *Adapter) SetServiceResolver(resolver ServiceResolver) {
	a.resolver = resolver
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mimeType"`
}

type PromptDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

type PromptMessage struct {
	Role    string      `json:"role"`
	Content ContentItem `json:"content"`
}

type PromptResult struct {
	Description string          `json:"description"`
	Messages    []PromptMessage `json:"messages"`
}

type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ContentItem struct {
	Type string         `json:"type"`
	Text string         `json:"text"`
	Meta map[string]any `json:"_meta,omitempty"`
}

func Tools() []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(registeredTools.ordered))
	for _, registration := range registeredTools.ordered {
		definitions = append(definitions, ToolDefinition{
			Name:        registration.name,
			Description: registration.description,
			InputSchema: withAgentAttribution(registration.schema()),
		})
	}
	return definitions
}

// withAgentAttribution mutates an InputSchema in place to declare the
// reserved _agent_model (string, required) and _agent_session_id (string,
// optional) fields. Centralising the injection here keeps every tool
// schema in lockstep with extractAgentAttribution's contract — adding a
// new tool registration cannot forget the attribution fields.
func withAgentAttribution(schema map[string]any) map[string]any {
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props[agentModelArgKey] = stringSchema(agentModelSchemaDescription)
	props[agentSessionArgKey] = stringSchema(agentSessionSchemaDescription)
	schema["properties"] = props

	required, _ := schema["required"].([]string)
	if !containsString(required, agentModelArgKey) {
		required = append(required, agentModelArgKey)
	}
	schema["required"] = required
	return schema
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func Resources() []ResourceDefinition {
	return []ResourceDefinition{
		{URI: "omakiten://project/overview", Name: "Active project overview", Description: "Compact overview of the active Omakiten project.", MIMEType: "application/json"},
		{URI: "omakiten://workflow/active", Name: "Active workflow", Description: "Active workflow buckets and transitions for the current Omakiten runtime.", MIMEType: "application/json"},
	}
}

// promptArguments declares the per-command argument list. Keeping it next to
// Prompts() means new commands only need a description in the agent layer
// plus, optionally, an entry here when they take inputs.
var promptArguments = map[string][]PromptArgument{
	"okt-shape":             {{Name: "topic", Description: "Raw idea, backlog, or shaping focus", Required: false}},
	"okt-run":               {{Name: "target", Description: "Optional task id, plan id, or plan slug to run", Required: false}},
	"okt-audit":             {{Name: "target", Description: "Optional task id, plan id, plan slug, or branch/diff target to audit", Required: false}},
	"okt-task-imagine":      {{Name: "topic", Description: "Topic, problem, or feature seed to explore", Required: false}},
	"okt-task-research":     {{Name: "task_id", Description: "Optional task id or topic to research", Required: false}},
	"okt-task-validate":     {{Name: "task_id", Description: "Optional task id or framing to validate", Required: false}},
	"okt-task-requirements": {{Name: "task_id", Description: "Optional task id or candidate to capture requirements for", Required: false}},
	"okt-task-prioritize":   {{Name: "task_id", Description: "Optional task id or candidate set to prioritize", Required: false}},
	"okt-task-create":       {{Name: "description", Description: "Task description", Required: true}},
	"okt-task-decompose":    {{Name: "task_id", Description: "Task id or coarse work item to decompose", Required: false}},
	"okt-task-estimate":     {{Name: "task_id", Description: "Task id or increment set to estimate", Required: false}},
	"okt-task-design":       {{Name: "task_id", Description: "Task id to design", Required: false}},
	"okt-plan-create":       {{Name: "slug", Description: "Optional plan slug to create", Required: false}, {Name: "name", Description: "Optional plan name", Required: false}},
	"okt-plan-show":         {{Name: "slug", Description: "Plan slug", Required: true}},
	"okt-plan-continue":     {{Name: "slug", Description: "Plan slug", Required: true}},
	"okt-plan-claim":        {{Name: "slug", Description: "Plan slug", Required: true}},
	"okt-task-resume":       {{Name: "task_id", Description: "Task id", Required: true}},
	"okt-task-continue":     {{Name: "task_id", Description: "Task id", Required: true}},
	"okt-task-implement":    {{Name: "task_id", Description: "Task id to implement", Required: false}},
	"okt-task-self-review":  {{Name: "task_id", Description: "Optional task id whose diff is being self-reviewed", Required: false}, {Name: "base", Description: "Optional git diff base", Required: false}},
	"okt-task-refactor":     {{Name: "task_id", Description: "Optional task id or finding to refactor", Required: false}},
	"okt-task-document":     {{Name: "focus", Description: "Optional area to focus the survey (e.g. 'README', 'architecture')", Required: false}},
	"okt-task-debrief":      {{Name: "task_id", Description: "Optional completed task id to debrief", Required: false}},
	"okt-config":            {{Name: "focus", Description: "Optional config topic or file to inspect", Required: false}},
	"okt-skill":             {{Name: "slug", Description: "Optional skill slug to load", Required: false}},
	"okt-task-review":       {{Name: "task_id", Description: "Optional task id whose diff is being reviewed", Required: false}, {Name: "base", Description: "Optional git diff base", Required: false}},
	"okt-task-secure":       {{Name: "task_id", Description: "Optional task id whose diff is being security-reviewed", Required: false}, {Name: "base", Description: "Optional git diff base", Required: false}},
	"okt-task-check":        {{Name: "task_id", Description: "Optional task id whose checks are being run", Required: false}, {Name: "target", Description: "Optional specific check target", Required: false}},
	"okt-task-quality":      {{Name: "task_id", Description: "Optional task id whose diff is being quality-reviewed", Required: false}},
	"okt-pause":             {{Name: "body", Description: "Optional handoff body override", Required: false}, {Name: "note", Description: "Optional extra handoff context", Required: false}},
	"okt-note-free":         {{Name: "title", Description: "Note title", Required: false}, {Name: "body", Description: "Note body", Required: false}, {Name: "scope", Description: "Optional scope: project or global", Required: false}, {Name: "kind", Description: "Optional note kind", Required: false}},
	"okt-note-recap":        {{Name: "since", Description: "Optional recap window (e.g. 24h, 7d, day)", Required: false}, {Name: "kinds", Description: "Optional comma-separated kind filter", Required: false}, {Name: "project", Description: "Optional project slug", Required: false}, {Name: "limit", Description: "Optional per-project limit", Required: false}},
	"okt-note-list":         {{Name: "scope", Description: "Optional scope: project, global, or both", Required: false}, {Name: "kind", Description: "Optional kind filter", Required: false}, {Name: "tag", Description: "Optional tag filter", Required: false}, {Name: "pinned", Description: "Optional pinned-only filter", Required: false}},
	"okt-note-show":         {{Name: "id", Description: "Note/comment id", Required: true}},
}

// Prompts lists every `okt-*` prompt with its entity-sourced one-line
// description. The description is the frontmatter `description` of the command's
// bound okt-<slug>-playbook skill, resolved through the wired agent service —
// there is no hardcoded Go description anymore. When no service is wired (older
// runtimes / tests), descriptions fall back to empty; the names + arguments
// still list so prompts/list keeps functioning.
func (a *Adapter) Prompts() []PromptDefinition {
	var service *agent.Service
	if a != nil {
		service = a.defaultService()
	}
	names := agent.CommandNames()
	out := make([]PromptDefinition, 0, len(names))
	for _, name := range names {
		desc := ""
		if service != nil {
			desc = service.CommandDescription(name)
		}
		out = append(out, PromptDefinition{
			Name:        name,
			Description: desc,
			Arguments:   promptArguments[name],
		})
	}
	return out
}

func (a *Adapter) CallTool(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	service := a.defaultService()
	if service == nil {
		return ToolResult{}, fmt.Errorf("mcp adapter requires an agent service")
	}

	if args == nil {
		args = map[string]any{}
	}
	agentModel, agentSessionID, err := extractAgentAttribution(args)
	if err != nil {
		return ToolResult{}, err
	}

	if a.resolver != nil {
		project, projectID := peekProjectArg(args)
		if resolved, err := a.resolver(ctx, project, projectID); err == nil && resolved != nil {
			service = resolved
		}
	}

	ctx = activity.WithAgent(ctx, "mcp", name, agentModel, agentSessionID)
	return a.dispatchTool(ctx, service, name, args)
}

// peekProjectArg extracts `project` / `project_id` from an arbitrary
// tool input WITHOUT consuming them — handlers still decode the same
// keys via the input struct's embedded ProjectSelector. Numeric values
// arrive as float64 (json.Unmarshal default) or json.Number; both
// shapes are accepted.
func peekProjectArg(args map[string]any) (project string, projectID int64) {
	if raw, ok := args["project"]; ok {
		if s, ok := raw.(string); ok {
			project = s
		}
	}
	if raw, ok := args["project_id"]; ok {
		switch v := raw.(type) {
		case float64:
			projectID = int64(v)
		case int64:
			projectID = v
		case int:
			projectID = int64(v)
		case json.Number:
			if i, err := v.Int64(); err == nil {
				projectID = i
			}
		}
	}
	return project, projectID
}

// dispatchTool runs the bare tool dispatch with the activity context the
// caller already prepared. Splits out of CallTool so internal entry points
// (ReadResource) can bypass the coercive _agent_model validation — those
// calls are system-internal, not agent-driven, and shouldn't pollute the
// per-model metrics with synthetic samples.
//
// The service parameter is resolved by the caller (CallTool peeks the
// project arg and asks the resolver; ReadResource always uses the
// default). dispatch itself never reads a.service so per-project
// routing works without cross-call interference.
func (a *Adapter) dispatchTool(ctx context.Context, service *agent.Service, name string, args map[string]any) (ToolResult, error) {
	if a.repo != nil {
		ctx = activity.WithRepository(ctx, a.repo)
	}

	handler, ok := registeredTools.handlers[name]
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown MCP tool %q", name)
	}
	return handler(a, ctx, service, args)
}

func (a *Adapter) ReadResource(ctx context.Context, uri string) (ToolResult, error) {
	service := a.defaultService()
	if service == nil {
		return ToolResult{}, fmt.Errorf("mcp adapter requires an agent service")
	}
	// Resource reads are system-internal — no _agent_model validation.
	// Empty agent model marks them as "not benchmarked" so the metrics
	// layer can filter them out without a special sentinel.
	ctx = activity.WithAgent(ctx, "mcp", "resource:"+uri, "", "")
	switch uri {
	case "omakiten://project/overview":
		return a.dispatchTool(ctx, service, "project.overview", map[string]any{})
	case "omakiten://workflow/active":
		return a.dispatchTool(ctx, service, "workflow.show", map[string]any{})
	default:
		return ToolResult{}, fmt.Errorf("unknown MCP resource %q", uri)
	}
}

// GetPrompt resolves the bound persona, skills, laws, and templates for the
// requested `okt-*` command and returns a single PromptMessage carrying the
// composed markdown. Callers route through the adapter so the agent service's
// command catalog drives the response — when no service is wired (older
// runtimes / tests), GetPrompt falls back to the action text alone so the
// prompt still functions.
//
// When the bundle's `config.mcp.cache_prompts` is true (the default), the
// returned content carries a `_meta.anthropic.cache_control` hint so
// Anthropic-aware MCP clients can reuse the cached prompt across calls.
// Unaware clients simply ignore the metadata field — there is no protocol
// risk in always emitting it, but the toggle exists for users who want to
// observe pre/post caching behavior or work around a buggy client.
func (a *Adapter) GetPrompt(ctx context.Context, name string, args map[string]any) (PromptResult, error) {
	var service *agent.Service
	if a != nil {
		service = a.defaultService()
	}
	if service == nil {
		// No service wired (older runtimes / partial bootstraps). The command
		// playbook is entity-sourced now — there is no hardcoded Go action to
		// fall back to — so a registered command resolves to an empty,
		// registered-only message rather than fabricated prose, and an
		// unregistered name still errors. The cache hint rides along: the empty
		// body is byte-stable.
		if !agent.IsRegisteredCommand(name) {
			return PromptResult{}, fmt.Errorf("unknown MCP prompt %q", name)
		}
		return promptResult("", "", true), nil
	}
	resolved, err := service.ResolveCommand(ctx, agent.ResolveCommandInput{Name: name, Arguments: args})
	if err != nil {
		return PromptResult{}, err
	}
	return promptResult(resolved.Description, resolved.Markdown, service.SettingsCachePrompts()), nil
}

// resolveCommandTool backs the `commands.resolve` tool: it fetches the same
// composed playbook markdown the prompt path renders and returns it as raw
// text. The agent-callable twin of GetPrompt — both route through
// service.ResolveCommand so there is a single source of truth and the bytes
// match (AC#4). An unknown/invalid name surfaces as a structured IsError tool
// result, mirroring how every other tool reports a domain error.
func resolveCommandTool(ctx context.Context, service *agent.Service, args map[string]any) (ToolResult, error) {
	name, _ := args["name"].(string)
	var arguments map[string]any
	if raw, ok := args["arguments"].(map[string]any); ok {
		arguments = raw
	}
	resolved, err := service.ResolveCommand(ctx, agent.ResolveCommandInput{Name: name, Arguments: arguments})
	if err != nil {
		return resultFromData(agent.FailureFromError(err), true)
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: resolved.Markdown}}}, nil
}

func promptResult(description, body string, cacheControl bool) PromptResult {
	content := ContentItem{Type: "text", Text: body}
	if cacheControl {
		content.Meta = map[string]any{
			"anthropic.cache_control": map[string]string{"type": "ephemeral"},
		}
	}
	return PromptResult{Description: description, Messages: []PromptMessage{{Role: "user", Content: content}}}
}

func decodeArgs(args map[string]any, out any) error {
	if args == nil {
		args = map[string]any{}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func resultFromData(data any, isError bool) (ToolResult, error) {
	text, err := jsonText(data)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: text}}, IsError: isError}, nil
}

func jsonText(data any) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func selectorSchema() map[string]any {
	return objectSchema(selectorProperties(), nil)
}

func editProjectSchema() map[string]any {
	props := selectorProperties()
	props["description"] = stringSchema("New project description text")
	return objectSchema(props, []string{"description"})
}

func showTemplateSchema() map[string]any {
	props := selectorProperties()
	props["slug"] = stringSchema("Template slug")
	return objectSchema(props, []string{"slug"})
}

func selectorProperties() map[string]any {
	return map[string]any{
		"project_id": integerSchema("Optional registered project id"),
		"project":    stringSchema("Optional registered project slug"),
		"cwd":        stringSchema("Optional working directory used for project resolution"),
	}
}

func createTaskSchema() map[string]any {
	props := selectorProperties()
	props["title"] = stringSchema("Optional task title; derived from description when omitted")
	props["description"] = stringSchema("Task description or title text")
	props["priority"] = stringSchema("Optional priority: low, normal, or high")
	props["confirmed"] = booleanSchema("Set true after user confirmation")
	props["template_slug"] = stringSchema("Optional slug of a loaded template; when set, the template body is merged into the description (user content first, template appended). Use templates.list to discover slugs.")
	props["parent_id"] = nullableIntegerSchema("Optional parent task id. Set to an existing task's id to attach the new row as a sub-task via TaskService.AddSub (parent must belong to the same project and be active; cross-bucket parents are rejected). Omit or pass null to create a root task.")
	return objectSchema(props, []string{"description"})
}

func dependencySchema(includeConfirmed bool) map[string]any {
	props := selectorProperties()
	props["task_id"] = integerSchema("Task id")
	props["depends_on_task_id"] = integerSchema("Dependency task id")
	if includeConfirmed {
		props["confirmed"] = booleanSchema("Required true to remove the dependency")
	}
	return objectSchema(props, []string{"task_id", "depends_on_task_id"})
}

func progressSchema() map[string]any {
	props := selectorProperties()
	props["task_id"] = integerSchema("Task id for task edits, comments, or movement")
	props["title"] = stringSchema("Optional updated title")
	props["description"] = stringSchema("Optional updated description")
	props["priority"] = stringSchema("Optional priority: low, normal, or high")
	props["move_to_bucket"] = stringSchema("Optional target workflow bucket key")
	props["comment"] = stringSchema("Optional progress comment")
	props["author_type"] = stringSchema("human or agent")
	return objectSchema(props, nil)
}

func tagMutationSchema(includeTagID bool) map[string]any {
	props := selectorProperties()
	props["entity_type"] = stringSchema("Entity type: 'task' or 'project'")
	props["entity_id"] = integerSchema("Entity id (task_id for tasks; omit for projects)")
	if includeTagID {
		props["tag_id"] = integerSchema("Tag id to remove")
		props["confirmed"] = booleanSchema("Required true to remove the tag")
		return objectSchema(props, []string{"entity_type", "tag_id"})
	}
	props["tag_name"] = stringSchema("Tag name (normalized to kebab-case automatically)")
	return objectSchema(props, []string{"entity_type", "tag_name"})
}

func tagListSchema() map[string]any {
	props := selectorProperties()
	props["entity_type"] = stringSchema("Entity type: 'task' or 'project'")
	props["entity_id"] = integerSchema("Entity id (task_id for tasks; omit for projects)")
	return objectSchema(props, []string{"entity_type"})
}

// commandResolveSchema declares the param surface for the `commands.resolve`
// tool: a required okt-* command slug plus an optional freeform arguments
// object passed through to ResolveCommand exactly as the prompt path passes
// prompts/get arguments.
func commandResolveSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":      stringSchema("okt-* command slug to resolve (e.g. \"okt-audit\", \"okt-run\", \"okt-task-implement\"). Discover the full set via commands.list."),
		"arguments": objectValueSchema("Optional command arguments passed through to the playbook, equivalent to the prompt path (e.g. {\"task_id\": 42} or {\"target\": \"1175\"})."),
	}, []string{"name"})
}

// objectValueSchema declares a freeform JSON object property (arbitrary keys),
// used for pass-through argument bags whose shape varies per command.
func objectValueSchema(description string) map[string]any {
	return map[string]any{"type": "object", "description": description}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func ftsQuerySchema() map[string]any {
	return map[string]any{
		"type":        "string",
		"maxLength":   domain.SearchQueryMaxBytes,
		"description": "FTS5 MATCH expression over indexed text. Maximum 4096 UTF-8 bytes and 256 lexical terms/operators.",
	}
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

// nullableIntegerSchema declares an optional integer that also accepts the JSON
// null literal. The schema-validating MCP client passes integers as-is, keeps
// `null` (instead of stripping it as "unknown"), and drops the property entirely
// when the caller leaves it absent — preserving the tri-state encoding the
// agent layer relies on for fields like tasks.parent_id (`omitted` /
// `null` / `id`).
func nullableIntegerSchema(description string) map[string]any {
	return map[string]any{"type": []string{"integer", "null"}, "description": description}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func arrayStringSchema(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}

func recordErrorSchema() map[string]any {
	props := selectorProperties()
	props["description"] = stringSchema("One-line description of the error")
	props["context"] = stringSchema("Optional surrounding context, stack trace, or symptoms")
	props["tags"] = arrayStringSchema("Tag names attached to this error (normalized to kebab-case). Use specific tags so future searches can match — e.g. [\"sqlite\", \"foreign-key\"].")
	return objectSchema(props, []string{"description"})
}

func searchSchema() map[string]any {
	props := selectorProperties()
	props["query"] = ftsQuerySchema()
	props["entity_types"] = arrayStringSchema("Optional restriction to a subset of entity types. Allowed: task, comment, error, solution, plan. Empty or omitted indexes all five.")
	return objectSchema(props, []string{"query"})
}

func addSolutionSchema() map[string]any {
	props := selectorProperties()
	props["error_id"] = integerSchema("Error id to attach the solution to")
	props["description"] = stringSchema("One-line description of the candidate solution")
	props["steps"] = stringSchema("Optional explicit steps, code snippet, or command to apply the solution")
	props["task_id"] = integerSchema("Optional task id where this solution was discovered/applied")
	return objectSchema(props, []string{"error_id", "description"})
}

func confirmSolutionSchema() map[string]any {
	props := selectorProperties()
	props["solution_id"] = integerSchema("Solution id to confirm")
	props["success"] = booleanSchema("true if the solution worked (also increments the solution's like counter); false to mark it as known-bad")
	return objectSchema(props, []string{"solution_id", "success"})
}

func listTopSolutionsSchema() map[string]any {
	props := selectorProperties()
	props["limit"] = integerSchema("Maximum number of solutions to return (defaults and caps come from config.solutions; omitted/<=0 = default_top_limit, larger values clamped to max_top_limit)")
	return objectSchema(props, nil)
}

// logsListSchema declares the param surface for the `logs.list` tool.
// All four params are optional so the no-arg call returns a useful
// default response (last window_days of every category, descending).
// Categories enumerates the legal EventCategory string values so
// schema-aware clients (and the human reading mcp.md) can drive the
// chip without re-deriving them from the Go enum.
func logsListSchema() map[string]any {
	props := selectorProperties()
	categoryEnum := make([]string, 0, len(domain.KnownEventCategories))
	for _, c := range domain.KnownEventCategories {
		categoryEnum = append(categoryEnum, string(c))
	}
	props["categories"] = map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string", "enum": categoryEnum},
		"description": "Optional list of EventCategory values to include. Empty/omitted = every category. Example: [\"tool_call\"] reproduces the legacy activity-log filter; [\"task\", \"comment\"] narrows to task lifecycle and comments.",
	}
	props["since"] = stringSchema("Optional time floor as a Go duration (\"24h\", \"30m\") or N-day shorthand (\"7d\", \"30d\"). Omitted → use the project's configured Logs window (config.views.logs.window_days, 30 days by default).")
	props["limit"] = integerSchema("Optional row cap. 0/omitted/>10000 = capped at 10000 by the SQL layer's safety ceiling.")
	props["order"] = stringSchema("Sort direction: \"desc\" (default, newest first) or \"asc\" (oldest first). Anything else falls back to \"desc\".")
	return objectSchema(props, nil)
}
