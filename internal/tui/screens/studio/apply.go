package studio

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	bundledraft "omakiten/internal/config/bundledraft"
	"omakiten/internal/domain"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/overlay"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

// ApplyOverlayOpen reports whether the apply candidate overlay is showing.
// The host uses this to switch the header into Overlay mode; the card itself
// is painted by Screen.View.
func (s Screen) ApplyOverlayOpen() bool { return s.studioApplyOpen }

func (s Screen) OwnsFooter() bool      { return s.studioApplyOpen }
func (s Screen) BlocksHostInput() bool { return s.studioApplyOpen }

// OwnsKey is the sub-screen's own vocabulary plus whatever the arranger will
// consume, read off screenlayout.StandardBindings rather than restated.
//
// Restating it is how Flow shipped an OwnsKey missing all eight of the scroll
// keys its handler accepted, so the host routed none of them and the body below
// the transition matrix could not be reached at all (#2416). A screen that reads
// the table cannot be short by one, and a spelling added to the table reaches
// every Studio sub-screen without an edit here.
//
// While the apply overlay is open, esc and ctrl+s are claimed so the host
// routes them here instead of treating esc as back.
func (s Screen) OwnsKey(msg tea.KeyMsg) bool {
	key := msg.String()
	if s.studioApplyOpen && (key == "esc" || key == "ctrl+s") {
		return true
	}
	own, isStudio := studioOwnKeys[s.id]
	if !isStudio {
		return false
	}
	return studioKeyIn(key, own...) || studioKeyIn(key, studioScrollKeys()...)
}

func (s *Screen) handleStudioApplyOverlayKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		s.dismissStudioApplyOverlay()
	case "ctrl+s":
		s.applyStudioCandidate(s.studioPreviewReport())
	}
	return nil
}

func (s *Screen) openStudioApplyOverlay() {
	s.studioApplyOpen = true
	s.applyStudioCandidate(s.studioPreviewReport())
}

func (s *Screen) dismissStudioApplyOverlay() {
	s.studioApplyOpen = false
	s.studioApplyMsg = ""
	s.invalidateStudioConfirmation()
}

func (s *Screen) setApplyMessage(msg string) {
	s.studioApplyMsg = msg
	switch s.id {
	case screenhost.StudioWorkflow:
		s.studioWorkflowMsg = msg
	case screenhost.StudioCommands:
		s.studioCommandMsg = msg
	case screenhost.StudioPersonas:
		s.studioPersonaMsg = msg
	case screenhost.StudioHooks:
		s.studioHookMsg = msg
	}
}

func (s *Screen) applyStudioCandidate(report bundledraft.Report) {
	if s.studioDraft == nil {
		s.setApplyMessage(s.tr("tui.studio.preview.msg.no_changes", "no Studio changes to apply"))
		return
	}
	if !report.Dirty {
		s.setApplyMessage(s.tr("tui.studio.preview.msg.candidate_clean", "candidate is clean"))
		return
	}
	if report.ValidationError != nil {
		s.setApplyMessage(s.tr("tui.studio.preview.msg.apply_disabled", "apply disabled: %s", report.ValidationError.Error()))
		return
	}
	if report.BlockedReason != "" {
		s.setApplyMessage(s.tr("tui.studio.preview.msg.apply_disabled", "apply disabled: %s", report.BlockedReason))
		return
	}
	assurance, err := s.studioDraft.AssuranceSnapshot()
	if err != nil {
		s.invalidateStudioConfirmation()
		s.setApplyMessage(s.tr("tui.studio.preview.msg.apply_disabled", "apply disabled: %s", err.Error()))
		return
	}
	impact := studioImpactLines(s.t, s.studioDraft.ImpactPreview(s.ctx, studioTaskCounter(s.tasks)))
	confirmation := studioConfirmationSnapshot(report, impact, assurance)
	if !s.studioApplyArmed || s.studioApplyConfirmation != confirmation {
		s.studioApplyArmed = true
		s.studioApplyConfirmation = confirmation
		if len(impact) > 0 {
			s.setApplyMessage(s.tr("tui.studio.preview.msg.impact_arm", "impact warnings present; press ctrl+s again to apply this candidate"))
		} else {
			s.setApplyMessage(s.tr("tui.studio.preview.msg.press_again", "press ctrl+s again to apply this candidate"))
		}
		return
	}
	if _, err := s.studioDraft.Apply(s.ctx, func(path string) error {
		if s.reload == nil {
			return nil
		}
		return s.reload(path, report.DiffSummary)
	}); err != nil {
		s.invalidateStudioConfirmation()
		s.setApplyMessage(s.tr("tui.studio.preview.msg.apply_failed", "apply failed: %s", err.Error()))
		return
	}
	s.invalidateStudioConfirmation()
	s.studioApplyOpen = false
	s.setApplyMessage(s.tr("tui.studio.preview.msg.applied", "Studio candidate applied and runtime reloaded"))
}

func studioConfirmationSnapshot(report bundledraft.Report, impact []string, assurance string) string {
	payload, _ := json.Marshal(struct {
		Candidate config.Bundle
		Impact    []string
		Assurance string
	}{report.Candidate, impact, assurance})
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func (s *Screen) invalidateStudioConfirmation() {
	s.studioApplyArmed = false
	s.studioApplyConfirmation = ""
}

func (s Screen) paintStudioApplyOverlay(base string) string {
	if !s.studioApplyOpen {
		return base
	}
	width := s.kit.Width
	if width <= 0 {
		width = 80
	}
	// Overlay onto the body rectangle the host already reserved. Padding to
	// kit.Rows() (full terminal height) overdrew ChromeRows+footer and pushed
	// the keybinding row off screen.
	height := screenkit.BlockRows(base)
	if height < 1 {
		height = 1
	}
	cardWidth := width - 8
	if cardWidth > 104 {
		cardWidth = 104
	}
	if cardWidth < 40 {
		cardWidth = width
		if cardWidth < 24 {
			cardWidth = 24
		}
	}
	card := overlay.RenderConfirm(s.studioApplyConfirm(cardWidth))
	return overlay.Overlay(padViewRectangle(base, width, height), card, overlay.PositionCenter)
}

func (s Screen) studioApplyConfirm(width int) overlay.Confirm {
	report := s.studioPreviewReport()
	none := s.tr("tui.studio.preview.none", "none")
	kicker := s.styles.Kicker(s.tr("tui.studio.apply.kicker", "APPLY CANDIDATE"))
	if report.Dirty {
		kicker += s.styles.Info.Render(fmt.Sprintf(" · %d %s", len(report.DiffSummary), s.tr("tui.studio.apply.edits", "edits")))
	}
	validation := none
	if report.ValidationError != nil {
		validation = screenkit.Sanitize(report.ValidationError.Error())
	}
	diff := s.tr("tui.studio.diff.no_changes", "No changes")
	if report.Dirty && len(report.DiffSummary) > 0 {
		diff = screenkit.Sanitize(report.DiffSummary[0])
	}
	warnings := append([]string(nil), report.EditWarnings...)
	warnings = append(warnings, s.projection.FlowWarnings...)
	warnings = append(warnings, s.projection.CommandWarnings...)
	warn := none
	if len(warnings) > 0 {
		warn = screenkit.Sanitize(warnings[0])
	}
	impactLines := []string(nil)
	if s.studioDraft != nil {
		impactLines = studioImpactLines(s.t, s.studioDraft.ImpactPreview(s.ctx, studioTaskCounter(s.tasks)))
	}
	impact := s.tr("tui.studio.apply.impact_none", "no pending bucket impact")
	if len(impactLines) > 0 {
		impact = screenkit.Sanitize(impactLines[0])
	}
	return overlay.Confirm{
		Width: width,
		Styles: overlay.Styles{
			Info:      s.styles.Info,
			Hint:      s.styles.Hint,
			Key:       s.styles.HintAccent,
			Footer:    s.styles.Hint,
			Separator: s.styles.Separator,
		},
		Kicker: kicker,
		Hint:   s.tr("tui.studio.apply.hint", "ctrl+s applies · esc discards"),
		Fields: []overlay.ConfirmField{
			{Label: s.tr("tui.studio.preview.label.state", "state"), Value: s.studioDirtyStateLabel()},
			{Label: s.tr("tui.studio.preview.label.validation", "validation"), Value: validation},
			{Label: s.tr("tui.studio.apply.label.diff", "diff"), Value: diff},
			{Label: s.tr("tui.studio.apply.label.warn", "warn"), Value: warn},
			{Label: s.tr("tui.studio.apply.label.impact", "impact"), Value: impact},
		},
		ListTitle: s.tr("tui.studio.preview.warnings", "WARNINGS"),
		ListItems: warnings,
		ListEmpty: s.tr("tui.studio.preview.warnings_empty", "No candidate warnings."),
		Message:   s.studioApplyMsg,
		Footer:    s.tr("tui.studio.apply.card_footer", "ctrl+s apply   esc discard"),
	}
}

func padViewRectangle(view string, width, height int) string {
	if width < 1 {
		width = 1
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		w := ansi.StringWidth(line)
		switch {
		case w < width:
			lines[i] = line + strings.Repeat(" ", width-w)
		case w > width:
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	if len(lines) > height && height > 0 {
		lines = lines[:height]
	}
	blank := strings.Repeat(" ", width)
	for len(lines) < height {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
var _ screenhost.KeyOwner = Screen{}

// studioPreviewReport answers from the draft the screen opened at entry. The
// dump Preview tab is gone; the apply overlay is the remaining consumer.
func (m Screen) studioPreviewReport() bundledraft.Report {
	if m.studioDraft != nil {
		return m.studioDraft.ReportText(m.t)
	}
	if snap := m.repos.activeSnapshot(); snap != nil {
		bundle := config.Bundle{Commands: snap.Commands(), Skills: snap.Skills(), AllSkills: snap.AllSkills(), Laws: snap.Laws(), AllLaws: snap.AllLaws(), Personas: snap.Personas(), AllPersonas: snap.AllPersonas(), Templates: snap.Templates(), AllTemplates: snap.AllTemplates(), Workflows: []config.Workflow{snapshotWorkflowToConfig(snap.Workflow())}, Config: config.Settings{Workflow: config.WorkflowSettings{Active: snap.Workflow().Key}}}
		return bundledraft.Report{Original: bundle, Candidate: bundle, DiffSummary: []string{m.tr("tui.studio.diff.no_changes", "No changes")}}
	}
	return bundledraft.Report{DiffSummary: []string{m.tr("tui.studio.diff.no_changes", "No changes")}}
}

func studioImpactLines(text bundledraft.Text, impact bundledraft.ImpactPreview) []string {
	var lines []string
	for _, bucket := range impact.RemovedBuckets {
		lines = append(lines, tr(text, "tui.studio.preview.impact.bucket_removed", "bucket removed: %s (%d active tasks)", bucket.Key, bucket.ActiveTaskCount))
	}
	for _, change := range impact.BucketKeyChanges {
		lines = append(lines, tr(text, "tui.studio.preview.impact.bucket_key_changed", "bucket key changed: %s -> %s (%d active tasks currently in %s)", change.FromKey, change.ToKey, change.ActiveTaskCount, change.FromKey))
	}
	if impact.FinalBucketChanged != nil {
		lines = append(lines, tr(text, "tui.studio.preview.impact.final_bucket_changed", "final bucket changed: %s -> %s", impact.FinalBucketChanged.FromKey, impact.FinalBucketChanged.ToKey))
	}
	if impact.TaskCountError != nil {
		lines = append(lines, tr(text, "tui.studio.preview.impact.task_count_failed", "task-count lookup failed: %s", impact.TaskCountError.Error()))
	}
	return lines
}

type studioTaskCounter []domain.Task

func (c studioTaskCounter) CountActiveTasksByBucket(_ context.Context, keys []string) (map[string]int, error) {
	want := map[string]struct{}{}
	for _, key := range keys {
		want[key] = struct{}{}
	}
	out := map[string]int{}
	for _, task := range c {
		if _, ok := want[task.BucketKey]; ok {
			out[task.BucketKey]++
		}
	}
	return out, nil
}

func snapshotWorkflowToConfig(workflow domain.Workflow) config.Workflow {
	return studioprojection.ConfigWorkflowFromDomain(workflow, nil)
}
