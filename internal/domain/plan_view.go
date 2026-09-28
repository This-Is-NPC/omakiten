package domain

// PlanShow is the aggregated view PlanService.Show returns. The active
// wave is the lowest-position wave whose tasks are not all in the
// workflow's final bucket; ActiveWaveID is 0 when every wave is done
// (or when the plan has no waves yet). Dependencies enumerates the
// in-plan task→task edges (both endpoints belong to this plan) so the
// network renderer can draw blocker markers without a follow-up query.
type PlanShow struct {
	Plan         Plan             `json:"plan"`
	Waves        []PlanWaveView   `json:"waves"`
	DoneCount    int              `json:"done_count"`
	TotalCount   int              `json:"total_count"`
	ActiveWaveID int64            `json:"active_wave_id,omitempty"`
	Dependencies []TaskDependency `json:"dependencies,omitempty"`
}

// PlanWaveView pairs a wave with its tasks and per-wave done/total
// counts. Used by the TUI network diagram and by agent plans.show.
type PlanWaveView struct {
	Wave       PlanWave      `json:"wave"`
	Tasks      []PlanTaskRow `json:"tasks,omitempty"`
	DoneCount  int           `json:"done_count"`
	TotalCount int           `json:"total_count"`
}

// PlanRollup is the lightweight per-plan projection the TUI list view
// consumes — slug/name/status from Plan plus the aggregated done/total
// counters and the active wave's display name. Waves and per-task detail
// stay out of this projection so callers do not pay the per-task scan
// cost for a one-line row.
type PlanRollup struct {
	Plan           Plan   `json:"plan"`
	DoneCount      int    `json:"done_count"`
	TotalCount     int    `json:"total_count"`
	ActiveWaveID   int64  `json:"active_wave_id,omitempty"`
	ActiveWaveName string `json:"active_wave_name,omitempty"`
}
