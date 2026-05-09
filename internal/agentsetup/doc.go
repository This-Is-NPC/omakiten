// Package agentsetup wires okt into the supported AI harnesses (Claude
// Code, Claude Desktop, OpenCode, Crush, Codex, GitHub Copilot) by
// rewriting their MCP server config files. Idempotent — safe to re-run on
// every install. Pure file I/O on user config locations; no network.
package agentsetup
