package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/output"
	"omakiten/internal/tui/components/notification"
)

// handleNotificationAction executes the cobra command an action declared (if
// any), updates m.status with the outcome, and refreshes the task snapshot so
// the Board reflects mutations the command made. Empty Command actions are
// treated as labeled dismissals — they record nothing and leave state alone.
//
// Audit: a non-empty Command emits a confirmation.granted event keyed by the
// active project before the dispatch fires, so the activity log captures the
// human keystroke that authorised the run.
func (m *Model) handleNotificationAction(action notification.ActionMsg) {
	if len(action.Command) == 0 {
		m.status = fmt.Sprintf(m.t("tui.status.notification_fmt"), action.Slug, action.ActionID)
		return
	}
	if m.repos.DispatchCommand == nil {
		m.status = fmt.Sprintf(m.t("tui.status.notification_skipped_fmt"), action.ActionID)
		return
	}

	m.emitConfirmationGranted(action)

	raw, err := m.repos.DispatchCommand(m.ctx, action.Command)
	envelope, parseErr := parseLastEnvelope(raw)
	switch {
	case parseErr != nil:
		// Cobra completed but emitted output we cannot read as the
		// standard envelope — surface the raw error so the user can
		// diagnose without leaving the TUI.
		m.status = fmt.Sprintf(m.t("tui.status.notification_output_unparsable_fmt"), action.ActionID, parseErr)
	case !envelope.OK:
		code := envelope.Code
		if code == "" {
			code = "command_failed"
		}
		m.status = fmt.Sprintf("%s: %s", code, envelope.Message)
	default:
		m.status = m.notificationActionStatus(action, envelope)
		if err := m.refresh(); err != nil {
			m.status = err.Error()
		}
	}
	if err != nil && envelope.OK {
		// Cobra Execute can return an exitError wrapper even when the
		// envelope itself signalled success — log the discrepancy in the
		// status so a reviewer notices.
		m.status += fmt.Sprintf(" (dispatch warning: %v)", err)
	}
}

// emitConfirmationGranted records that the human user pressed an action key
// and the corresponding command is about to run. Failure to record is
// swallowed so audit gaps do not block the user-visible side effect.
func (m *Model) emitConfirmationGranted(action notification.ActionMsg) {
	if m.project.ID == 0 || m.repos.Events == nil {
		return
	}
	payload := struct {
		NotificationSlug string   `json:"notification_slug"`
		ActionID         string   `json:"action_id"`
		Command          []string `json:"command"`
	}{
		NotificationSlug: action.Slug,
		ActionID:         action.ActionID,
		Command:          action.Command,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = m.repos.Events.RecordEntityEvent(m.ctx, domain.EventEntitySystem, 0, m.project.ID, domain.EventTypeConfirmationGranted, string(raw))
}

// parseLastEnvelope finds the trailing JSON envelope cobra wrote to stdout
// and unmarshals it. Cobra commands always end with exactly one envelope
// (via runJSON → output.Write); intermediate log noise is rare but tolerated
// by scanning from the last newline backwards.
func parseLastEnvelope(raw []byte) (output.Envelope, error) {
	trimmed := strings.TrimRight(string(raw), "\n")
	if trimmed == "" {
		return output.Envelope{}, fmt.Errorf("no output")
	}
	lastNL := strings.LastIndexByte(trimmed, '\n')
	jsonLine := trimmed
	if lastNL >= 0 {
		jsonLine = trimmed[lastNL+1:]
	}
	var envelope output.Envelope
	if err := json.Unmarshal([]byte(jsonLine), &envelope); err != nil {
		return output.Envelope{}, err
	}
	return envelope, nil
}

// notificationActionStatus picks the user-facing status line for a successful
// action. Falls back to a generic summary when the command's envelope data
// does not surface a friendly message field.
func (m Model) notificationActionStatus(action notification.ActionMsg, envelope output.Envelope) string {
	if msg, ok := m.envelopeMessage(envelope); ok {
		return msg
	}
	return fmt.Sprintf(m.t("tui.status.notification_action_applied_fmt"), action.ActionID, strings.Join(action.Command, " "))
}

func (m Model) envelopeMessage(envelope output.Envelope) (string, bool) {
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		return "", false
	}
	if v, ok := data["message"].(string); ok && v != "" {
		return v, true
	}
	if report, ok := data["report"].(map[string]any); ok {
		if total, ok := report["total"].(float64); ok && total > 0 {
			return fmt.Sprintf(m.t("tui.status.tasks_migrated_fmt"), int(total)), true
		}
	}
	return "", false
}
