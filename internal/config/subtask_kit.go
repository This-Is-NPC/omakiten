package config

const subtaskKitTransparencyNoticeKey = "notice.subtask_kit.enabled.mcp_resolves_at_root"

// SubtaskKitTransparencyNoticeKey returns the i18n catalog key used by UI
// surfaces that explain the protocol boundary when subtask_kit is first enabled.
func SubtaskKitTransparencyNoticeKey() string {
	return subtaskKitTransparencyNoticeKey
}

// NewSubtaskKitNoticeNeeded reports the one-shot transition that should surface
// the transparency notice: an already-loaded project moves from no sub-kit to a
// configured sub-kit. Boot without a previous snapshot and every other transition
// (same-path reload, sub-kit swap, disable) returns false.
func NewSubtaskKitNoticeNeeded(prev, next *Snapshot) bool {
	if prev == nil || next == nil {
		return false
	}
	return prev.SubtaskKitPath() == "" && next.SubtaskKitPath() != ""
}
