// Package mcp is the JSON-RPC server adapter that exposes Omakiten as an
// MCP (Model Context Protocol) endpoint. The Adapter dispatches
// tools/list, tools/call, resources/list, resources/read, prompts/list,
// and prompts/get against the agent-service composition. Every coded
// domain error is mapped to a JSON-RPC error payload that carries the
// stable code + details the agent can branch on; unknown errors degrade
// to a generic "internal error" so SQL fragments / file paths never leak
// to the client.
package mcp
