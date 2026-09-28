package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"omakiten/internal/domain"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// errorPayload converts a service error into a JSON-RPC error payload that is
// safe and useful to surface to the agent. Domain CodedErrors get a stable
// machine-readable code + the validation message + optional details. Anything
// else degrades to a generic "internal error" so we don't leak file paths,
// SQL fragments, or stack-shaped strings to the client. The original error
// text is intentionally dropped on the unknown path — debugging signal lives
// in the server's stderr (handled at a higher layer), not in the wire format.
func errorPayload(err error) *rpcError {
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		return &rpcError{
			Code:    jsonRPCCodeFor(coded.Code),
			Message: coded.Message,
			Data:    map[string]any{"code": string(coded.Code), "details": coded.Details},
		}
	}
	return &rpcError{Code: -32603, Message: "internal error"}
}

// jsonRPCCodeFor maps a domain.ErrorCode onto the JSON-RPC error
// code category the spec expects. Three buckets:
//   - validation (-32602, "Invalid params"): client supplied a
//     malformed argument the server can describe back to the caller.
//   - business  (-32000, server-defined): the request was well-formed
//     but the domain refused to satisfy it (not-found, guard
//     violation, conflict). Stable per-server custom code.
//   - internal  (-32603, "Internal error"): the server hit a
//     condition it can't blame on the caller.
//
// Today every CodedError collapsed to -32602; agents that wanted to
// distinguish "I sent garbage" from "the server says no" had to
// re-inspect the Data.code field. The per-category mapping makes
// the JSON-RPC code itself informative.
func jsonRPCCodeFor(code domain.ErrorCode) int {
	switch code {
	case domain.ErrValidation,
		domain.ErrDependencyInvalid,
		domain.ErrTagConflict:
		return -32602
	case domain.ErrConfigInvalid,
		domain.ErrConfigTooLarge,
		domain.ErrEditorFailed,
		domain.ErrEditorNotFound,
		domain.ErrUninstallFailed,
		domain.ErrUpdateFailed:
		return -32603
	default:
		// Business-rule rejections (not-found, conflict, guard,
		// workflow-invalid) live here. Any new domain code lands on
		// the business bucket by default; promote to validation /
		// internal as new categories appear.
		return -32000
	}
}

const toolsListChangedMethod = "notifications/tools/list_changed"

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func Serve(ctx context.Context, input io.Reader, output io.Writer, adapter *Adapter) error {
	return ServeNotify(ctx, input, output, adapter, nil)
}

// ServeNotify is Serve plus a channel that emits the MCP
// notifications/tools/list_changed notification (no id, no params)
// when the surfaces table rotates. The send side must not block —
// typically a buffered channel with a default-drop send from
// BundleCache.Reload. In-flight tools/call handlers finish; allow()
// is at method start, so a rotate never aborts a write in progress.
//
// Clients that ignore the notification still fail closed: the next
// tools/call hits allow() and is denied.
func ServeNotify(ctx context.Context, input io.Reader, output io.Writer, adapter *Adapter, toolsChanged <-chan struct{}) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := &lockedWriter{w: output}
	startToolsListChangedPump(ctx, out, toolsChanged)

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var request rpcRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			if err := writeRPC(out, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}); err != nil {
				return err
			}
			continue
		}

		response, shouldRespond := handleRPC(ctx, adapter, request)
		if !shouldRespond {
			continue
		}
		if err := writeRPC(out, response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func startToolsListChangedPump(ctx context.Context, output io.Writer, toolsChanged <-chan struct{}) {
	if toolsChanged == nil {
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-toolsChanged:
				if !ok {
					return
				}
				_ = writeJSONLine(output, rpcNotification{
					JSONRPC: "2.0",
					Method:  toolsListChangedMethod,
				})
			}
		}
	}()
}

func handleRPC(ctx context.Context, adapter *Adapter, request rpcRequest) (rpcResponse, bool) {
	respond := len(request.ID) > 0
	base := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	if strings.HasPrefix(request.Method, "notifications/") {
		return rpcResponse{}, false
	}

	switch request.Method {
	case "initialize":
		base.Result = initializeResult()
	case "ping":
		base.Result = map[string]any{}
	case "tools/list":
		base.Result = map[string]any{"tools": adapter.Tools()}
	case "tools/call":
		base.Result, base.Error = handleToolsCall(ctx, adapter, request.Params)
	case "resources/list":
		base.Result = map[string]any{"resources": Resources()}
	case "resources/read":
		base.Result, base.Error = handleResourceRead(ctx, adapter, request.Params)
	case "prompts/list":
		base.Result = map[string]any{"prompts": adapter.Prompts()}
	case "prompts/get":
		base.Result, base.Error = handlePromptGet(ctx, adapter, request.Params)
	default:
		base.Error = &rpcError{Code: -32601, Message: fmt.Sprintf("method %q not found", request.Method)}
	}

	return base, respond
}

func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools":     map[string]any{"listChanged": true},
			"resources": map[string]any{},
			"prompts":   map[string]any{},
		},
		"serverInfo": map[string]any{"name": "omakiten", "version": "dev"},
	}
}

func handleToolsCall(ctx context.Context, adapter *Adapter, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid tools/call params"}
	}
	result, err := adapter.CallTool(ctx, params.Name, params.Arguments)
	if err != nil {
		return nil, errorPayload(err)
	}
	return result, nil
}

func handleResourceRead(ctx context.Context, adapter *Adapter, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid resources/read params"}
	}
	result, err := adapter.ReadResource(ctx, params.URI)
	if err != nil {
		return nil, errorPayload(err)
	}
	return map[string]any{"contents": result.Content}, nil
}

func handlePromptGet(ctx context.Context, adapter *Adapter, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid prompts/get params"}
	}
	result, err := adapter.GetPrompt(ctx, params.Name, params.Arguments)
	if err != nil {
		return nil, errorPayload(err)
	}
	return result, nil
}

func writeRPC(output io.Writer, response rpcResponse) error {
	return writeJSONLine(output, response)
}

func writeJSONLine(output io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(data))
	return err
}
