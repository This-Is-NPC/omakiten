package config

import (
	"strings"
	"testing"
)

func TestValidateTricksAcceptsEmpty(t *testing.T) {
	if err := validateTricksSettings(TricksSettings{}, nil); err != nil {
		t.Fatalf("validateTricksSettings(empty) error = %v", err)
	}
}

func TestValidateTricksAcceptsValidNavOverride(t *testing.T) {
	tricks := TricksSettings{Nav: map[string]string{"33": "settings.personas"}}
	if err := validateTricksSettings(tricks, nil); err != nil {
		t.Fatalf("validateTricksSettings(valid) error = %v", err)
	}
}

func TestValidateTricksRejectsMalformedCode(t *testing.T) {
	cases := []string{"0a", "123", "1", "00", "01", "10", "9a"}
	for _, code := range cases {
		tricks := TricksSettings{Nav: map[string]string{code: "tasks.board"}}
		err := validateTricksSettings(tricks, nil)
		if err == nil {
			t.Errorf("validateTricksSettings(code %q) error = nil, want malformed", code)
			continue
		}
		if !strings.Contains(err.Error(), "malformed") {
			t.Errorf("validateTricksSettings(code %q) error = %v, want malformed message", code, err)
		}
	}
}

func TestValidateTricksRejectsEmptyRoute(t *testing.T) {
	tricks := TricksSettings{Nav: map[string]string{"11": "   "}}
	err := validateTricksSettings(tricks, nil)
	if err == nil {
		t.Fatalf("validateTricksSettings(empty route) error = nil")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("error = %v, want non-empty mention", err)
	}
}

func TestValidateTricksRejectsReservedVerbInHook(t *testing.T) {
	for _, verb := range []string{"nav", "op"} {
		hooks := []HookSpec{{
			On:   "trick.executed",
			When: map[string]string{"verb": verb, "operand": "1"},
			Do:   "exec",
		}}
		err := validateTricksSettings(TricksSettings{}, hooks)
		if err == nil {
			t.Errorf("validateTricksSettings(hook with verb=%q) error = nil, want reserved", verb)
			continue
		}
		if !strings.Contains(err.Error(), "reserved") {
			t.Errorf("verb=%q error = %v, want reserved message", verb, err)
		}
		if !strings.Contains(err.Error(), verb) {
			t.Errorf("verb=%q error = %v, want verb name in message", verb, err)
		}
	}
}

func TestValidateTricksAcceptsUserDefinedVerbInHook(t *testing.T) {
	hooks := []HookSpec{{
		On:   "trick.executed",
		When: map[string]string{"verb": "hook", "operand": "1"},
		Do:   "exec",
	}}
	if err := validateTricksSettings(TricksSettings{}, hooks); err != nil {
		t.Fatalf("validateTricksSettings(user-defined verb) error = %v", err)
	}
}

func TestValidateTricksIgnoresHooksWithoutVerbFilter(t *testing.T) {
	hooks := []HookSpec{
		{On: "task.moved", Do: "exec"},
		{On: "trick.executed", When: map[string]string{"operand": "1"}, Do: "exec"},
	}
	if err := validateTricksSettings(TricksSettings{}, hooks); err != nil {
		t.Fatalf("validateTricksSettings(no verb filter) error = %v", err)
	}
}
