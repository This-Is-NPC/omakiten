package operation

// PersonaInfo, SkillInfo, LawInfo, TemplateInfo are the agent-facing snapshots
// of bundle entities used by ResolveCommand. They mirror the relevant subset
// of internal/config types without importing it, so the agent layer stays
// protocol- and config-neutral.

// MCPCommandsGlobalKey mirrors config.MCPCommandsGlobalKey on the agent side.
// Duplicating it here avoids a config import while keeping the contract
// stable: the runtime emits the same key.
const MCPCommandsGlobalKey = "global"
