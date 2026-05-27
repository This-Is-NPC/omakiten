package tui

const (
	entityListWidth = 28
	// entityCardCellWidth is the on-screen footprint of one rendered card
	// — `entityListWidth` plus the 1-cell border on each side. The grid
	// packer uses this (not entityListWidth) when computing how many
	// cards fit side-by-side, so the wrap math matches the actual cell
	// width Lipgloss draws.
	entityCardCellWidth = entityListWidth + 2
)

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

// entityScreenMode tracks which sub-surface the entity screen is showing.
// Closed = no overlay; View = entity detail; the *Picker variants are the
// modal pickers that float on top of the config view.
type entityScreenMode int

const (
	entityScreenClosed entityScreenMode = iota
	entityScreenView
	entityScreenSkillPicker
	entityScreenThemePicker
	entityScreenConfigPicker
	entityScreenDefaultPicker
	entityScreenSubtaskKitPicker
)

// entityForm carries the per-screen state. For Phase 1 it only holds the
// detail-view target and (for personas) the skill picker selection set.
// Cursor + scroll for the embedded picker live on m.entityPicker — they
// were promoted out of this struct when picker.Model was introduced so
// the picker component owns its own state.
type entityForm struct {
	kind         entityKind
	mode         entityScreenMode
	slug         string
	pickerChecks map[string]bool
}

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

func (k entityKind) plural() string {
	return k.String() + "s"
}

func entityKinds() []entityKind {
	return []entityKind{entityKindLaw, entityKindPersona, entityKindSkill, entityKindTemplate, entityKindTag}
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

func (m Model) selectedEntityIndex(kind entityKind) int {
	if m.entityCursors == nil {
		return 0
	}
	cursor := m.entityCursors[kind]
	count := m.entityCount(kind)
	if count == 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= count {
		return count - 1
	}
	return cursor
}

func (m *Model) clampEntityCursor() {
	if m.entityCursors == nil {
		m.entityCursors = map[entityKind]int{}
	}
	for _, kind := range entityKinds() {
		count := m.entityCount(kind)
		cursor := m.entityCursors[kind]
		switch {
		case count == 0:
			m.entityCursors[kind] = 0
		case cursor < 0:
			m.entityCursors[kind] = 0
		case cursor >= count:
			m.entityCursors[kind] = count - 1
		}
	}
}
