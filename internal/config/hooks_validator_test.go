package config

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestValidateHooksHappyPath(t *testing.T) {
	specs := []HookSpec{
		{On: domain.EventTypeGuardViolated, Do: "exec", Args: map[string]any{"argv": []any{"echo", "hi"}}},
		{On: domain.EventTypeTaskCreated, Do: "noop"},
	}
	if err := ValidateHooks(specs, func(name string) bool { return name == "exec" || name == "noop" }, nil); err != nil {
		t.Fatalf("ValidateHooks = %v", err)
	}
}

func TestValidateHooksRejectsUnknownEventType(t *testing.T) {
	specs := []HookSpec{{On: "task.unknown", Do: "noop"}}
	err := ValidateHooks(specs, func(string) bool { return true }, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown event_type "task.unknown"`) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateHooksRejectsUnknownAction(t *testing.T) {
	specs := []HookSpec{{On: domain.EventTypeTaskCreated, Do: "ghost"}}
	err := ValidateHooks(specs, func(name string) bool { return name == "exec" }, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown action "ghost"`) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateHooksExecRequiresArgv(t *testing.T) {
	specs := []HookSpec{{On: domain.EventTypeTaskCreated, Do: "exec"}}
	err := ValidateHooks(specs, func(string) bool { return true }, nil)
	if err == nil || !strings.Contains(err.Error(), "args.argv") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateHooksExecArgvMustBeStrings(t *testing.T) {
	specs := []HookSpec{{On: domain.EventTypeTaskCreated, Do: "exec", Args: map[string]any{"argv": []any{"echo", 7}}}}
	err := ValidateHooks(specs, func(string) bool { return true }, nil)
	if err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateHooks_notificationHookMessageFromHookOK(t *testing.T) {
	specs := []HookSpec{{
		On:      domain.EventTypeGuardViolated,
		Notification:   "kit",
		Message: "from-hook",
	}}
	notifications := map[string]Notification{"kit": {Name: "kit"}}
	if err := ValidateHooks(specs, func(string) bool { return true }, notifications); err != nil {
		t.Fatalf("hook-only message should be accepted, got %v", err)
	}
}

func TestValidateHooks_notificationHookNoMessageAnywhere(t *testing.T) {
	specs := []HookSpec{{On: domain.EventTypeGuardViolated, Notification: "kit"}}
	notifications := map[string]Notification{"kit": {Name: "kit"}}
	err := ValidateHooks(specs, func(string) bool { return true }, notifications)
	if err == nil || !strings.Contains(err.Error(), "no message") {
		t.Fatalf("expected combined-presence error, got %v", err)
	}
}

func TestValidateHooks_notificationHookExclusiveMessage(t *testing.T) {
	specs := []HookSpec{{
		On: domain.EventTypeGuardViolated, Notification: "kit",
		Message: "x", MessageField: "y",
	}}
	notifications := map[string]Notification{"kit": {Name: "kit"}}
	err := ValidateHooks(specs, func(string) bool { return true }, notifications)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected exclusivity error, got %v", err)
	}
}
