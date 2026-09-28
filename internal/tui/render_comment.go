package tui

import (
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/field"
)

func (m Model) renderCommentInput() string {
	lines := []string{
		m.styles.kicker(m.t("tui.kicker.new_comment")),
		m.formHint(m.t("tui.form.hint.enter_saves"), m.t("tui.form.hint.alt_newline"), m.t("tui.form.hint.esc_cancels")),
	}
	if m.status != "" && m.status != m.t("tui.input.comment_body") {
		lines = append(lines, m.styles.statusBadge(m.status))
	}
	lines = append(lines, field.RenderArea(m.commentInput, m.commentInputWidth(), commentInputHeight, true, m.styles.multilineFormTheme()))
	return m.renderPanel(strings.Join(lines, "\n"))
}

func (m Model) renderCommentCardSelected(comment domain.Comment, focused bool) string {
	styles := m.styles.screenStyles()
	tags := make([]string, len(comment.Tags))
	for i, tag := range comment.Tags {
		tags[i] = tag.Label
	}
	return card.Painter{Styles: styles}.Comment(card.Comment{
		Author:    comment.AuthorType,
		Timestamp: comment.CreatedAt,
		Body:      comment.Body,
		EmptyText: m.t("tui.comment.empty"),
		Tags:      tags,
		MoreFmt:   m.t("tui.event.more_lines_fmt"),
		Focused:   focused,
		Width:     m.commentCardWidth(),
	})
}
