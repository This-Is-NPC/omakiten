package buddy

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func sampleSnapshot() BundleSnapshot {
	return BundleSnapshot{
		ActiveBuddy: "kit",
		Buddies: map[string]config.Buddy{
			"kit": sampleBuddy(),
		},
	}
}

func validArgs() map[string]any {
	return map[string]any{
		ArgAnimation:       "idle",
		ArgPosition:        string(PositionTopRight),
		ArgTypingMsPerChar: 25,
		ArgMessageField:    "hint",
		ArgDismiss: map[string]any{
			DismissArgMode: string(DismissModeKey),
			DismissArgKeys: []any{"esc"},
		},
	}
}

func TestShowAction_Name(t *testing.T) {
	a := NewShowAction(sampleSnapshot())
	if a.Name() != ActionName {
		t.Fatalf("Name() = %q, want %q", a.Name(), ActionName)
	}
}

func TestShowAction_NoProgramIsNoop(t *testing.T) {
	a := NewShowAction(sampleSnapshot())
	err := a.Execute(context.Background(), domain.Event{Body: "hi"}, validArgs())
	if err != nil {
		t.Fatalf("Execute with nil program returned error: %v", err)
	}
}

func TestParseArgs_rejectsMissingAnimation(t *testing.T) {
	args := validArgs()
	delete(args, ArgAnimation)
	_, err := ParseArgs(args)
	if err == nil || !strings.Contains(err.Error(), ArgAnimation) {
		t.Fatalf("expected error about animation, got %v", err)
	}
}

func TestParseArgs_rejectsBadPosition(t *testing.T) {
	args := validArgs()
	args[ArgPosition] = "nowhere"
	_, err := ParseArgs(args)
	if err == nil || !strings.Contains(err.Error(), "position") {
		t.Fatalf("expected error about position, got %v", err)
	}
}

func TestParseArgs_rejectsNegativeTyping(t *testing.T) {
	args := validArgs()
	args[ArgTypingMsPerChar] = -1
	_, err := ParseArgs(args)
	if err == nil || !strings.Contains(err.Error(), ">= 0") {
		t.Fatalf("expected error about typing >= 0, got %v", err)
	}
}

func TestParseArgs_rejectsTimeoutWithoutAfterMs(t *testing.T) {
	args := validArgs()
	args[ArgDismiss] = map[string]any{DismissArgMode: string(DismissModeTimeout)}
	_, err := ParseArgs(args)
	if err == nil || !strings.Contains(err.Error(), DismissArgAfterMs) {
		t.Fatalf("expected error about after_ms, got %v", err)
	}
}

func TestParseArgs_rejectsKeyWithoutKeys(t *testing.T) {
	args := validArgs()
	args[ArgDismiss] = map[string]any{DismissArgMode: string(DismissModeKey)}
	_, err := ParseArgs(args)
	if err == nil || !strings.Contains(err.Error(), DismissArgKeys) {
		t.Fatalf("expected error about keys, got %v", err)
	}
}

func TestParseArgs_acceptsTimeoutMode(t *testing.T) {
	args := validArgs()
	args[ArgDismiss] = map[string]any{
		DismissArgMode:    string(DismissModeTimeout),
		DismissArgAfterMs: 5000,
	}
	parsed, err := ParseArgs(args)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if parsed.Dismiss.AfterMs != 5000 {
		t.Fatalf("after_ms = %d, want 5000", parsed.Dismiss.AfterMs)
	}
}

func TestParseArgs_acceptsFrameIntervalOverride(t *testing.T) {
	args := validArgs()
	args[ArgFrameIntervalMs] = 250
	parsed, err := ParseArgs(args)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if parsed.FrameIntervalMs != 250 {
		t.Fatalf("FrameIntervalMs = %d, want 250", parsed.FrameIntervalMs)
	}
}

func TestResolveMessage_payloadFieldWins(t *testing.T) {
	ev := domain.Event{Body: "fallback", Payload: `{"hint": "from-payload"}`}
	got, err := resolveMessage(ev, "hint")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "from-payload" {
		t.Fatalf("got %q, want from-payload", got)
	}
}

func TestResolveMessage_fallsBackToBody(t *testing.T) {
	ev := domain.Event{Body: "fallback", Payload: `{"other": "x"}`}
	got, err := resolveMessage(ev, "hint")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
}

func TestResolveMessage_emptyEverythingErrors(t *testing.T) {
	ev := domain.Event{}
	_, err := resolveMessage(ev, "hint")
	if err == nil {
		t.Fatalf("expected error for empty event")
	}
}

func TestValidateShowArgs_requiresActiveBuddy(t *testing.T) {
	err := ValidateShowArgs(validArgs(), nil)
	if err == nil || !strings.Contains(err.Error(), "config.tui.buddy.active") {
		t.Fatalf("expected error about active buddy, got %v", err)
	}
}

func TestValidateShowArgs_rejectsUnknownAnimation(t *testing.T) {
	err := ValidateShowArgs(validArgs(), map[string]struct{}{"deny": {}})
	if err == nil || !strings.Contains(err.Error(), `animation "idle" not declared`) {
		t.Fatalf("expected unknown animation error, got %v", err)
	}
}

func TestValidateShowArgs_passesWhenAnimationKnown(t *testing.T) {
	err := ValidateShowArgs(validArgs(), map[string]struct{}{"idle": {}})
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}
