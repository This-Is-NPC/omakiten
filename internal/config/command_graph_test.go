package config

import "testing"

func TestCommandGraphRejectsBrokenReferences(t *testing.T) {
	bundle := Bundle{
		Skills:   []Skill{{Slug: "shape", Command: &SkillCommand{Name: "shape", Next: []CommandReference{{Name: "run", Context: "full"}}}}},
		Commands: map[string]CommandSpec{"shape": {Skills: []string{"shape"}}},
	}
	if err := validateCommandSkills(bundle); err == nil {
		t.Fatal("missing related command was accepted")
	}
	bundle.Skills = append(bundle.Skills, Skill{Slug: "run", Command: &SkillCommand{Name: "run"}})
	bundle.Commands["run"] = CommandSpec{Skills: []string{"run"}}
	if err := validateCommandSkills(bundle); err != nil {
		t.Fatalf("valid graph rejected: %v", err)
	}
	bundle.Skills[0].Command.Next[0].Context = "recursive"
	if err := validateCommandSkills(bundle); err == nil {
		t.Fatal("unsupported context mode was accepted")
	}
}
