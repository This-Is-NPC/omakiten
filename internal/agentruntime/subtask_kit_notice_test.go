package agentruntime

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestBundleCacheFiresSubtaskKitNoticeOnFirstEnablement(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	if _, err := rt.Cache().Resolve(ctx, rt.defaultProjectID, rt.configPath); err != nil {
		t.Fatalf("seed Resolve: %v", err)
	}
	// No sub-kit yet: no transparency notice on the first build.
	if events, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeSubtaskKitNoticeEmitted, 10); err != nil {
		t.Fatalf("ListRecentEvents seed: %v", err)
	} else if len(events) != 0 {
		t.Fatalf("seed reload emitted notice; events=%+v", events)
	}

	// Transition: no sub-kit → izakaya.yaml. Helper must fire.
	appendRuntimeTopLevelYAML(t, rt.configPath, "subtask_kit: izakaya.yaml\n")
	if _, err := rt.Cache().Reload(ctx, rt.defaultProjectID, rt.configPath); err != nil {
		t.Fatalf("Reload enabling sub-kit: %v", err)
	}
	events, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeSubtaskKitNoticeEmitted, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents after enable: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 transparency notice event, got %d", len(events))
	}
	if !strings.Contains(events[0].Payload, config.SubtaskKitTransparencyNoticeKey()) {
		t.Fatalf("payload missing i18n key: %q", events[0].Payload)
	}
	if !strings.Contains(events[0].Payload, `"to_kit":"izakaya"`) {
		t.Fatalf("payload missing to_kit: %q", events[0].Payload)
	}

	// Same-path reload: helper must NOT refire — still exactly one event.
	if _, err := rt.Cache().Reload(ctx, rt.defaultProjectID, rt.configPath); err != nil {
		t.Fatalf("Reload same path: %v", err)
	}
	again, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeSubtaskKitNoticeEmitted, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents after same-path reload: %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("expected notice to remain at 1 event after same-path reload, got %d", len(again))
	}
}
