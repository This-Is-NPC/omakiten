package app

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// RenderNoteTemplate renders a note template body (Go text/template syntax)
// against the supplied data and collapses the empty lines that
// `{{if .Slot}}...{{end}}` blocks leave behind when a slot is empty.
//
// The four v1 note templates (`note-handoff`, `note-recap`,
// `note-standup-digest`, `note-free`) share the same engine: slots are
// referenced as `{{.SlotName}}` and conditional sections wrap their
// heading + body in `{{if .Slot -}} ... {{end -}}` so an empty slot
// erases the whole section. The renderer is intentionally minimal — the
// note commands that will land in N4/#359 W3 are responsible for shaping
// the slot data; the template only enforces layout.
//
// Returns an error when the template fails to parse or a referenced slot
// is mis-spelt (text/template surfaces both as Execute errors).
func RenderNoteTemplate(name, body string, data any) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", nil
	}
	tpl, err := template.New(name).Option("missingkey=zero").Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse note template %q: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render note template %q: %w", name, err)
	}
	return collapseBlankRuns(buf.String()), nil
}

// collapseBlankRuns squashes runs of 3+ consecutive newlines down to two
// so a section whose `{{if .Slot}}` block evaluated false does not leave
// a stack of blank lines between its neighbours. The two-newline target
// preserves Markdown paragraph breaks and keeps `---` separators visible.
func collapseBlankRuns(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blanks++
			if blanks <= 1 {
				out = append(out, "")
			}
			continue
		}
		blanks = 0
		out = append(out, line)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}
