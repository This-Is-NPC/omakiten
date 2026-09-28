package studio

import (
	"omakiten/internal/config"
	"omakiten/internal/studioprojection"
)

func projectionText(text Text) studioprojection.Text {
	return func(key, fallback string, args ...any) string { return tr(text, key, fallback, args...) }
}

// refreshProjection is called from bind, lifecycle, and update paths. Views
// consume this prepared value and never build catalog or workflow facts.
func (s *Screen) refreshProjection() {
	bundle := s.projectionBundle()
	s.projection = studioprojection.Build(studioprojection.Input{
		Bundle:       bundle,
		Workflow:     s.workflow,
		Snapshot:     s.repos.Snapshot,
		Tasks:        s.tasks,
		CommandNames: s.repos.CommandNames,
		HookHistory:  s.hookHistory,
		Text:         projectionText(s.t),
	})
	s.projectionReady = true
	s.projectionSnapshot = s.repos.Snapshot
	s.projectionTaskHead = taskHead(s.tasks)
	s.projectionTaskCount = len(s.tasks)
}

func (s Screen) projectionBundle() config.Bundle {
	if s.studioDraft != nil {
		return s.studioDraft.Candidate()
	}
	if snap := s.repos.activeSnapshot(); snap != nil {
		return config.Bundle{
			Kit:       snap.Kit(),
			Workflows: []config.Workflow{studioprojection.ConfigWorkflowFromDomain(s.workflow, snap)},
			Personas:  snap.Personas(), AllPersonas: snap.AllPersonas(),
			Skills: snap.Skills(), AllSkills: snap.AllSkills(),
			Laws: snap.Laws(), AllLaws: snap.AllLaws(),
			Templates: snap.Templates(), AllTemplates: snap.AllTemplates(),
			MCPCommands: snap.MCPCommands(),
			Config:      snap.Settings(),
		}
	}
	return config.Bundle{Workflows: []config.Workflow{studioprojection.ConfigWorkflowFromDomain(s.workflow, nil)}}
}
