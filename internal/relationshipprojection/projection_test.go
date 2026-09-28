package relationshipprojection

import (
	"testing"
)

func TestNormalizeOptionsAndSelection(t *testing.T) {
	got := NormalizeOptions(PersonaSkills, []Option{
		{Value: "z", Selected: true}, {Value: "a"}, {Value: "z"}, {Create: true},
	})
	if len(got) != 3 || got[0].Value != "a" || got[1].Value != "z" || !got[2].Create {
		t.Fatalf("normalized options = %#v", got)
	}
	if selected := SelectedValues(got); len(selected) != 1 || selected[0] != "z" {
		t.Fatalf("selected values = %#v", selected)
	}
	if selected := SelectedSet(got); !selected["z"] || len(selected) != 1 {
		t.Fatalf("selected set = %#v", selected)
	}
}

func TestTemplateDefaultOptions(t *testing.T) {
	got := TemplateDefaultOptions([]string{"task", "pr"}, "", "other", "active")
	if len(got) != 3 || !got[2].None || !got[2].Selected {
		t.Fatalf("synthetic clear option = %#v", got)
	}
}
