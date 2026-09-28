package studio

import (
	"fmt"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/screenkit"
)

func (m *Screen) ensureStudioDraft() error {
	if m.studioDraft != nil {
		return nil
	}
	if m.repos.Editor == nil || m.repos.OpenDraft == nil {
		return fmt.Errorf("%s", tr(m.t, "tui.studio.msg.editor_unavailable", "editor wiring unavailable; Studio mutations are disabled"))
	}
	draft, err := m.repos.OpenDraft(m.repos.Editor)
	if err != nil {
		return err
	}
	draft.BindText(m.t)
	m.studioDraft = draft
	return nil
}

func studioDraftReportMessage(report StudioDraftReport, ok string) string {
	if report.ValidationError != nil {
		return report.ValidationError.Error()
	}
	return ok
}

func StudioFlowWarnings(workflow config.Workflow, text Text) []string {
	return studioprojection.FlowWarnings(workflow, projectionText(text))
}

func configWorkflowFromDomain(workflow domain.Workflow, snap *config.Snapshot) config.Workflow {
	return studioprojection.ConfigWorkflowFromDomain(workflow, snap)
}

func prefixLines(lines []string, prefix string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, prefix+screenkit.Sanitize(line))
	}
	return out
}
