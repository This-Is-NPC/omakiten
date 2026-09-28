package config

import (
	"strings"
	"testing"
)

func TestPresetHookMessagesAllUseTokens(t *testing.T) {
	for _, preset := range ListPresets() {
		settings, err := LoadKitConfigByKey(preset.Name)
		if err != nil {
			t.Fatal(err)
		}
		for i, hook := range settings.Hooks {
			if !strings.HasPrefix(hook.Message, "${{intl:") {
				t.Errorf("preset %s hook[%d] message %q is not an intl token", preset.Name, i, hook.Message)
			}
		}
	}
}
