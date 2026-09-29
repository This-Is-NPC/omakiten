package config

import (
	"strings"
	"testing"
)

func TestPresetHookMessagesAllUseTokens(t *testing.T) {
	settings, err := LoadKitConfig()
	if err != nil {
		t.Fatal(err)
	}
	for i, hook := range settings.Hooks {
		if !strings.HasPrefix(hook.Message, "${{intl:") {
			t.Errorf("hook[%d] message %q is not an intl token", i, hook.Message)
		}
	}
}
