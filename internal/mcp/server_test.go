package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
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
