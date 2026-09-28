// Package app is the application-services layer. It owns workflow policy,
// bundle editing, dependency sync, template defaulting, and the read-model
// fan-out the TUI consumes. Every service in here depends only on
// (a) `internal/domain` for pure types and (b) port interfaces declared in
// `internal/app/ports.go` (and a few peer service files) — never on a
// concrete adapter package.
//
// # Hexagonal layering
//
//	+------------------------------------------------+
//	| internal/cli, internal/tui, internal/mcp       |   adapters (in)
//	| internal/agentruntime  ← composition root      |
//	+----+----------------+--------------------+-----+
//	     |                |                    |
//	     v                v                    v
//	+------------------------------------------------+
//	|         internal/operation (fachada)           |   application facade
//	|         internal/app (services + ports)        |
//	+----+--------+--------+--------+------+---------+
//	     |        |        |        |      |
//	     v        v        v        v      v
//	+------------------------------------------------+
//	| internal/domain (pure types, errors, IDs)      |   inner core
//	+------------------------------------------------+
//	     ^        ^        ^        ^      ^
//	     |        |        |        |      |
//	+----+--------+--------+--------+------+---------+
//	| internal/sqlite, internal/configstore          |   adapters (out)
//	+------------------------------------------------+
//
// Adapters on the right (sqlite, configstore) implement the ports declared
// here. The composition root (internal/agentruntime) constructs app
// services and injects the right-hand adapters as port-typed dependencies.
// CLI, TUI, and MCP consume operation.Service — they do not mount app
// services themselves (CLI: onda 3.1; TUI: onda 3.2).
//
// # Forbidden directions (enforced in internal/arch/arch_test.go)
//
//   - internal/domain MUST NOT import any adapter or app package — it sits
//     alone at the centre.
//   - internal/app MUST NOT import internal/sqlite, internal/configstore,
//     internal/tui, internal/cli, internal/mcp, internal/agent, or
//     internal/agentruntime — it talks to those layers via ports only.
//   - internal/sqlite and internal/configstore MUST NOT import each other,
//     internal/app, or any consumer adapter — they are leaves.
//
// The `.golangci.yml` mirrors these rules under `depguard` so editor +
// CI lints surface violations the same way.
//
// # Adding a new application service
//
//  1. Define the ports it needs in internal/app/ports.go (or a new
//     <service>_ports.go file alongside the service).
//  2. Implement the service against those ports.
//  3. In internal/sqlite or internal/configstore, add the methods the new
//     ports require — the *Store / *Adapter type already satisfies the
//     CompositeWorkflowStore / matching composite simply by virtue of
//     receivers existing.
//  4. Wire it up at the composition root (internal/agentruntime) and
//     expose it through operation.Service when it is a product/authoring
//     capability. CLI/TUI/MCP must not call New*Service themselves.
//  5. Run `go test ./internal/arch/...` to confirm boundaries are intact.
package app
