// Package hooks declares the YAML-driven hooks engine that subscribes
// to the in-process events bus and dispatches actions asynchronously.
// The engine is configured via config.hooks; actions register
// themselves through the Engine's RegisterAction method.
package hooks

// SubjectDepthFilter controls which task-subject depth a hook entry accepts.
// The zero value preserves legacy behavior: no depth filtering.
type SubjectDepthFilter uint8

const (
	SubjectDepthAny SubjectDepthFilter = iota
	SubjectDepthRoot
	SubjectDepthSubtask
)

// Hook is one entry of config.hooks. The engine matches incoming events
// against On (event_type) and When (top-level payload equality, AND
// across keys); when both match it dispatches Do with Args. Args is
// generic — each action interprets the keys it cares about; the per-
// action contract is documented in .docs/hooks.md.
type Hook struct {
	On           string                 `yaml:"on" json:"on"`
	When         map[string]string      `yaml:"when,omitempty" json:"when,omitempty"`
	Do           string                 `yaml:"do" json:"do"`
	Args         map[string]interface{} `yaml:"args,omitempty" json:"args,omitempty"`
	SubjectDepth SubjectDepthFilter     `yaml:"-" json:"-"`
	ResolvedKit  string                 `yaml:"-" json:"-"`
}
