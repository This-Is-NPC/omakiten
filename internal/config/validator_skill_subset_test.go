package config

import (
	"strings"
	"testing"
)

func bundleWithCommandSkills(repertoire, cmdSkills []string) Bundle {
	return Bundle{
		Personas: []Persona{
			{Slug: "engineer", SchemaVersion: 2, SkillRepertoire: repertoire},
		},
		MCPCommands: map[string]MCPCommandSpec{
			MCPCommandsGlobalKey: {Laws: []string{"template-fidelity"}},
			"okt-implement":      {Persona: "engineer", Skills: cmdSkills},
		},
	}
}

// TestSkillSubsetAcceptsSubset is the positive case: a command whose skills
// are all members of the persona's skill_repertoire validates clean.
func TestSkillSubsetAcceptsSubset(t *testing.T) {
	b := bundleWithCommandSkills([]string{"go", "sqlite", "markdown"}, []string{"go", "markdown"})
	if err := validateMCPCommandSkillSubset(b); err != nil {
		t.Fatalf("validateMCPCommandSkillSubset() = %v, want nil", err)
	}
}

// TestSkillSubsetRejectsSuperset is the negative case: a command selecting a
// skill outside the persona's repertoire is rejected, and the error names the
// command, the persona, and the missing skill.
func TestSkillSubsetRejectsSuperset(t *testing.T) {
	b := bundleWithCommandSkills([]string{"go", "sqlite"}, []string{"go", "rust", "cobol"})
	err := validateMCPCommandSkillSubset(b)
	if err == nil {
		t.Fatalf("validateMCPCommandSkillSubset() = nil, want rejection")
	}
	msg := err.Error()
	for _, want := range []string{"okt-implement", "engineer", "rust", "cobol"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

// TestSkillSubsetEmptyCommandSkillsIsClean confirms that a command with no
// skills imposes no constraint (legacy v1 commands skip the check).
func TestSkillSubsetEmptyCommandSkillsIsClean(t *testing.T) {
	b := bundleWithCommandSkills([]string{"go"}, nil)
	if err := validateMCPCommandSkillSubset(b); err != nil {
		t.Fatalf("validateMCPCommandSkillSubset() = %v, want nil for skill-less command", err)
	}
}

// TestSkillSubsetEmptyRepertoireRejectsAnySelection confirms a command that
// selects skills against a persona with an empty repertoire is rejected — the
// empty pool is a strict zero-member set, not a wildcard.
func TestSkillSubsetEmptyRepertoireRejectsAnySelection(t *testing.T) {
	b := bundleWithCommandSkills(nil, []string{"go"})
	if err := validateMCPCommandSkillSubset(b); err == nil {
		t.Fatalf("validateMCPCommandSkillSubset() = nil, want rejection against empty repertoire")
	}
}
