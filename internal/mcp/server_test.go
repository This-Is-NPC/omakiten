package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestServeInitialize(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error != nil {
		t.Fatalf("response error = %#v", response.Error)
	}
	result := response.Result.(map[string]any)
	if result["protocolVersion"] == nil {
		t.Fatal("initialize response missing protocolVersion")
	}
}

func TestServePing(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error != nil {
		t.Fatalf("response error = %#v", response.Error)
	}
}

func TestServeToolsCall(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"project.overview","arguments":{}}}` + "\n")
	var output bytes.Buffer
	if err := Serve(ctx, input, &output, NewAdapter(service)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error != nil {
		t.Fatalf("response error = %#v", response.Error)
	}
}

func TestServeToolsCallInvalidParams(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"invalid"}` + "\n")
	var output bytes.Buffer
	if err := Serve(ctx, input, &output, NewAdapter(service)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error == nil {
		t.Fatal("tools/call invalid params error = nil")
	}
}

func TestServeResourcesRead(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"omakiten://project/overview"}}` + "\n")
	var output bytes.Buffer
	if err := Serve(ctx, input, &output, NewAdapter(service)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error != nil {
		t.Fatalf("response error = %#v", response.Error)
	}
}

func TestServePromptsGet(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"okt"}}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error != nil {
		t.Fatalf("response error = %#v", response.Error)
	}
}

func TestServeUnknownMethod(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"unknown/method"}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error == nil {
		t.Fatal("unknown method error = nil")
	}
}

func TestServeParseError(t *testing.T) {
	input := strings.NewReader(`not json` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error == nil {
		t.Fatal("parse error = nil")
	}
}

func TestServeEmptyLineSkip(t *testing.T) {
	input := strings.NewReader(`
{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if response.Error != nil {
		t.Fatalf("response error = %#v", response.Error)
	}
}

func TestErrorPayloadCodedError(t *testing.T) {
	err := domain.NewError(domain.ErrTaskNotFound, "task not found", map[string]any{"task_id": 7})
	payload := errorPayload(err)
	if payload.Code != -32602 {
		t.Errorf("Code = %d, want -32602", payload.Code)
	}
	if payload.Message != "task not found" {
		t.Errorf("Message = %q, want %q", payload.Message, "task not found")
	}
	data, ok := payload.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data = %T, want map[string]any", payload.Data)
	}
	if data["code"] != string(domain.ErrTaskNotFound) {
		t.Errorf("Data.code = %v, want %s", data["code"], domain.ErrTaskNotFound)
	}
}

func TestErrorPayloadOpaqueError(t *testing.T) {
	// Generic errors must NOT leak the original message — the agent gets a
	// flat "internal error" with the JSON-RPC internal-error code so we
	// don't accidentally surface file paths, SQL fragments, or driver
	// strings to the caller.
	err := fmt.Errorf("open %s: permission denied", "/home/howl/.config/secret.yaml")
	payload := errorPayload(err)
	if payload.Code != -32603 {
		t.Errorf("Code = %d, want -32603", payload.Code)
	}
	if payload.Message != "internal error" {
		t.Errorf("Message = %q, want %q", payload.Message, "internal error")
	}
	if strings.Contains(payload.Message, ".config") || strings.Contains(payload.Message, "secret") {
		t.Errorf("Message leaked path: %q", payload.Message)
	}
	if payload.Data != nil {
		t.Errorf("Data = %v, want nil for opaque errors", payload.Data)
	}
}

func TestServeNotificationNoResponse(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("notification should produce no output, got %q", output.String())
	}
}
