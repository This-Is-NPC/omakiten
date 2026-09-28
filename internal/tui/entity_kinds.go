package tui

// entityKind enumerates the kinds the config view can display side-by-side.
// Order is the canonical horizontal order of the columns; the same order is
// used for the help screen and the section-scroll math.
type entityKind int

const (
	entityKindLaw entityKind = iota
	entityKindPersona
	entityKindSkill
	entityKindTemplate
	entityKindTag
)

func (k entityKind) String() string {
	switch k {
	case entityKindLaw:
		return "Law"
	case entityKindPersona:
		return "Persona"
	case entityKindSkill:
		return "Skill"
	case entityKindTemplate:
		return "Template"
	case entityKindTag:
		return "Tag"
	default:
		return ""
	}
}

func (m Model) entityCount(kind entityKind) int {
	switch kind {
	case entityKindLaw:
		return len(m.laws)
	case entityKindPersona:
		return len(m.personas)
	case entityKindSkill:
		return len(m.skills)
	case entityKindTemplate:
		return len(m.templates)
	case entityKindTag:
		return len(m.tags)
	}
	return 0
}
