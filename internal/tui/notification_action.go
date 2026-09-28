package tui

import (
	"encoding/json"
	"fmt"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// handleNotificationAction executes an operation intent and refreshes committed data.
func (m *Model) handleNotificationAction(action ActionMsg) {
	if action.Operation == "" {
		m.status = fmt.Sprintf(m.t("tui.status.notification_fmt"), action.Slug, action.ActionID)
		return
	}
	if m.repos.DispatchAction == nil {
		m.status = fmt.Sprintf(m.t("tui.status.notification_skipped_fmt"), action.ActionID)
		return
	}
	m.emitConfirmationGranted(action)
	result, err := m.repos.DispatchAction(m.ctx, contract.ActionRequest{Operation: action.Operation, Arguments: action.Arguments, ProjectID: m.project.ID, RuntimeID: m.repos.ProjectID})
	if err != nil {
		m.status = err.Error()
		return
	}
	switch {
	case result.Confirmation.RequiresConfirmation:
		m.status = result.Confirmation.Reason
	case result.Message != "":
		m.status = result.Message
	case result.Migration:
		m.status = fmt.Sprintf(m.t("tui.status.tasks_migrated_fmt"), result.Migrated)
	default:
		m.status = fmt.Sprintf(m.t("tui.status.notification_action_applied_fmt"), action.ActionID, action.Operation)
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	}
}

// emitConfirmationGranted records that the human user pressed an action key
// and the corresponding command is about to run. Failure to record is
// swallowed so audit gaps do not block the user-visible side effect.
func (m *Model) emitConfirmationGranted(action ActionMsg) {
	if m.project.ID == 0 || m.repos.Events == nil {
		return
	}
	payload := struct {
		NotificationSlug string         `json:"notification_slug"`
		ActionID         string         `json:"action_id"`
		Operation        string         `json:"operation"`
		Arguments        map[string]any `json:"arguments,omitempty"`
	}{
		NotificationSlug: action.Slug,
		ActionID:         action.ActionID,
		Operation:        action.Operation, Arguments: action.Arguments,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = m.repos.Events.RecordEntityEvent(m.ctx, domain.EventEntitySystem, 0, m.project.ID, domain.EventTypeConfirmationGranted, string(raw))
}
