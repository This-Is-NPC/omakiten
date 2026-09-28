package relationshipprojection

import "sort"

// Kind identifies the relationship being edited.
type Kind string

const (
	PersonaSkills   Kind = "persona-skills"
	TemplateDefault Kind = "template-default"
)

// Option is a persistence-neutral relationship choice.
type Option struct {
	Value    string
	Label    string
	Detail   string
	Selected bool
	None     bool
	Create   bool
}

// NormalizeOptions deduplicates and orders selectable persona skills. Template
// defaults retain caller order because their order is the configured kind order.
func NormalizeOptions(kind Kind, input []Option) []Option {
	options := append([]Option(nil), input...)
	if kind != PersonaSkills {
		return options
	}
	filtered := options[:0]
	seen := map[string]bool{}
	for _, option := range options {
		if option.Value == "" || option.Create || seen[option.Value] {
			continue
		}
		seen[option.Value] = true
		filtered = append(filtered, option)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Value < filtered[j].Value })
	return append(filtered, Option{Create: true})
}

// TemplateDefaultOptions builds the configured choices plus the explicit
// clear choice used by the template-default picker.
func TemplateDefaultOptions(kinds []string, currentKind, currentProject, activeProject string) []Option {
	out := make([]Option, 0, len(kinds)+1)
	for _, kind := range kinds {
		out = append(out, Option{Value: kind, Label: kind, Selected: currentKind == kind && currentProject == activeProject})
	}
	return append(out, Option{None: true, Selected: currentKind == "" || currentProject != activeProject})
}

// SelectedSet returns selected real relationship values.
func SelectedSet(options []Option) map[string]bool {
	out := map[string]bool{}
	for _, option := range options {
		if option.Selected && !option.None && !option.Create {
			out[option.Value] = true
		}
	}
	return out
}

// SelectedValues returns selected real relationship values in option order.
func SelectedValues(options []Option) []string {
	out := make([]string, 0, len(options))
	for _, option := range options {
		if option.Selected && !option.None && !option.Create {
			out = append(out, option.Value)
		}
	}
	return out
}

// SelectedIndex returns the first selected option or zero.
func SelectedIndex(options []Option) int {
	for i, option := range options {
		if option.Selected {
			return i
		}
	}
	return 0
}
