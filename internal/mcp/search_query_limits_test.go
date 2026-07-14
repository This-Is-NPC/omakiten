package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestMCPFTSSurfacesRejectAmplificationAtSharedCap(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"search", "comments.list"} {
		tool := tool
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			service := newMCPTestService(t, context.Background())
			query := strings.Repeat("界", domain.SearchQueryMaxBytes/3+1)
			result, err := NewAdapter(service).CallTool(context.Background(), tool, withModel(map[string]any{"query": query}))
			if err != nil {
				t.Fatalf("CallTool(%s): %v", tool, err)
			}
			if !result.IsError {
				t.Fatalf("oversized MCP %s returned success", tool)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
				t.Fatalf("decode MCP failure: %v", err)
			}
			if payload["code"] != string(domain.ErrValidation) || payload["message"] != "search query exceeds limits" {
				t.Fatalf("MCP failure = %#v, want stable shared-cap validation", payload)
			}
		})
	}
}

func TestMCPCommentsListMalformedFTSIsStableValidation(t *testing.T) {
	t.Parallel()
	service := newMCPTestService(t, context.Background())
	result, err := NewAdapter(service).CallTool(context.Background(), "comments.list", withModel(map[string]any{"query": `"unterminated`}))
	if err != nil {
		t.Fatalf("CallTool(comments.list): %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode MCP failure: %v", err)
	}
	if !result.IsError || payload["code"] != string(domain.ErrValidation) || payload["message"] != "invalid FTS5 query expression" || strings.Contains(result.Content[0].Text, "unterminated string") {
		t.Fatalf("malformed comments.list failure = %s", result.Content[0].Text)
	}
}
