package tui

import (
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/commentdetail"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/taskdetail"
	"omakiten/internal/tui/screens/taskform"
)

// staticProductSlugs maps a hosted screen id and a single key fragment
// onto a census slug. Keys that are navigation, scroll, help, or a
// TUI-shaped extra with no census row (SyncBlockers, entity
// Add/Edit/Remove, BoardSnapshot, …) are absent and stay visible.
//
// Unmapped product slugs with a TUI cell in the census — no footer/help
// identity to hide, or the TUI path is an extra rather than the census:
// command.list, command.resolve, comment.list, dependency.list, error.record,
// insights.summary, law.*, logs.list, metrics.summary, orphans.migrate
// (notification overlay, gated in the handler), persona.*, plan.continue,
// plan.create, plan.delete, plan.list, plan.show, plan.task.assign
// (no TUI path; plannetwork `c` is task.assign / assignee),
// plan.task.claim_next, plan.task.unassign, plan.wave.*, progress.record,
// project.edit, project.overview, search (palette, gated in
// dispatchPaletteSearch), skill.*, solution.*, tag.add, tag.list,
// tag.list_all, task.continue, task.create_intent, task.list,
// task_activity.list, template.*, workflow.show, wiring.*.
var dependencyProductSlugs = []string{"dependency.add", "dependency.remove"}

var staticProductSlugs = map[screenhost.ID]map[string]string{
	screenhost.TasksBoard: {
		"n": "task.create",
		"e": "task.edit",
		"c": "comment.add",
		"m": "task.transition",
	},
	screenhost.TasksTable: {
		"n": "task.create",
		"e": "task.edit",
		"c": "comment.add",
		"m": "task.transition",
	},
	screenhost.TasksGraph: {
		"n": "task.create",
		"e": "task.edit",
		"c": "comment.add",
		"m": "task.transition",
	},
	screenhost.TaskDetail: {
		"d": "task.delete",
		"e": "task.edit",
		"n": "task.create",
		"a": "task.create",
		"c": "comment.add",
		"m": "task.transition",
	},
	screenhost.CommentDetail: {
		"d":      "comment.delete",
		"e":      "comment.edit",
		"ctrl+s": "comment.edit",
	},
	screenhost.PlanNetwork: {
		"c": "task.assign",
		"e": "plan.edit",
	},
	screenhost.Project: {
		"R": "project.resume",
	},
	screenhost.SettingsTags: {
		"d": "tag.remove",
		"m": "tag.merge",
		"D": "tag.remove",
	},
}

// helpGroupProductSlugs covers hardcoded help rows in render_help.go
// that are not replaced by a screen Help() group. Screen-declared
// groups are filtered through staticProductSlugs instead.
var helpGroupProductSlugs = map[string]map[string]string{
	"task_view": {
		"e":     "task.edit",
		"a":     "task.create",
		"n":     "task.create",
		"c":     "comment.add",
		"m":     "task.transition",
		"d":     "task.delete",
		"space": "task.transition",
	},
	"blocker_picker": {
		"ctrl+s": "dependency.add",
	},
	"task_form": {
		"ctrl+b": "dependency.add",
	},
	"tasks_board": {
		"n": "task.create",
		"e": "task.edit",
		"c": "comment.add",
		"m": "task.transition",
	},
	"tasks_table": {
		"n": "task.create",
		"e": "task.edit",
		"m": "task.transition",
	},
	"comment_view": {
		"e": "comment.edit",
		"d": "comment.delete",
	},
	"comment_edit": {
		"ctrl+s": "comment.edit",
	},
	"settings_tags": {
		"d": "tag.remove",
		"m": "tag.merge",
		"D": "tag.remove",
	},
	"plan_network": {
		"c": "task.assign",
		"e": "plan.edit",
	},
}

var actionProductSlugs = map[screenhost.ActionKind]string{
	screenhost.ActionCreateTask:              "task.create",
	screenhost.ActionEditTask:                "task.edit",
	screenhost.ActionMoveTask:                "task.transition",
	screenhost.ActionMoveTaskToBucket:        "task.transition",
	screenhost.ActionMoveTaskFromDetail:      "task.transition",
	screenhost.ActionCreateSubtask:           "task.create",
	screenhost.ActionDeleteTaskFromDetail:    "task.delete",
	screenhost.ActionArchiveTaskFromDetail:   "task.archive",
	screenhost.ActionUnarchiveTaskFromDetail: "task.unarchive",
	screenhost.ActionAddTaskComment:          "comment.add",
	screenhost.ActionSaveComment:             "comment.edit",
	screenhost.ActionDeleteComment:           "comment.delete",
	screenhost.ActionSavePlanGoal:            "plan.edit",
	screenhost.ActionSetTaskAssignee:         "task.assign",
	screenhost.ActionOpenProjectResume:       "project.resume",
	screenhost.ActionPrepareTagDelete:        "tag.remove",
	screenhost.ActionDeleteTag:               "tag.remove",
	screenhost.ActionDeleteOrphanTags:        "tag.remove",
	screenhost.ActionPrepareTagMerge:         "tag.merge",
	screenhost.ActionMergeTags:               "tag.merge",
	screenhost.ActionCompleteSubtask:         "task.transition",
	screenhost.ActionSaveTaskBlockers:        "dependency.add",
	screenhost.ActionOpenTaskBlockers:        "dependency.add",
}

func (m Model) productSlugsForBinding(screen screenhost.Screen, key string) []string {
	if slugs := contextualProductSlugs(screen, key); len(slugs) > 0 {
		return slugs
	}
	if screen == nil {
		if slug := m.hostProductSlug(key); slug != "" {
			return []string{slug}
		}
		return nil
	}
	if slugs := dependencySlugsForKey(screen.ID(), key); len(slugs) > 0 {
		return slugs
	}
	if slug := staticProductSlugs[screen.ID()][key]; slug != "" {
		return []string{slug}
	}
	return nil
}

func (m Model) productSlugsForAction(kind screenhost.ActionKind, screen screenhost.Screen) []string {
	switch kind {
	case screenhost.ActionSaveTaskBlockers, screenhost.ActionOpenTaskBlockers:
		return dependencyProductSlugs
	case screenhost.ActionSaveTaskForm:
		if form, ok := screen.(taskform.Screen); ok && form.Payload().Mode == taskform.Edit {
			return []string{"task.edit"}
		}
		return []string{"task.create"}
	}
	if slug := actionProductSlugs[kind]; slug != "" {
		return []string{slug}
	}
	return nil
}

func dependencySlugsForKey(id screenhost.ID, key string) []string {
	switch {
	case id == screenhost.TaskDetail && key == "b":
		return dependencyProductSlugs
	case id == screenhost.TaskForm && key == "ctrl+b":
		return dependencyProductSlugs
	}
	return nil
}

func (m Model) hostProductSlug(key string) string {
	switch m.mode {
	case modeMove:
		if key == "enter" {
			return "task.transition"
		}
	case modeComment:
		if key == "enter" {
			return "comment.add"
		}
	}
	switch key {
	case "n":
		return "task.create"
	case "e":
		return "task.edit"
	case "m":
		return "task.transition"
	case "c":
		return "comment.add"
	}
	return ""
}

func textEntryAbsorbsKey(screen screenhost.Screen, key string) bool {
	if key == "ctrl+s" || key == "enter" {
		return false
	}
	switch s := screen.(type) {
	case taskdetail.Screen:
		return s.State().Mode != taskdetail.ModeNormal
	case taskform.Screen:
		return true
	case commentdetail.Screen:
		return s.Mode() == commentdetail.ModeEdit
	case plannetwork.Screen:
		return s.Mode() != plannetwork.ModeBrowse
	}
	return false
}

func contextualProductSlugs(screen screenhost.Screen, key string) []string {
	switch s := screen.(type) {
	case taskdetail.Screen:
		return taskDetailProductSlugs(s, key)
	case taskform.Screen:
		return taskFormProductSlugs(s, key)
	case plannetwork.Screen:
		return planNetworkProductSlugs(s, key)
	}
	return nil
}

func taskDetailProductSlugs(screen taskdetail.Screen, key string) []string {
	if key == "ctrl+s" && screen.State().Mode == taskdetail.ModeBlockers {
		return dependencyProductSlugs
	}
	if slug := taskDetailContextualSlug(screen, key); slug != "" {
		return []string{slug}
	}
	return nil
}

func taskFormProductSlugs(screen taskform.Screen, key string) []string {
	if key != "ctrl+s" {
		return nil
	}
	if screen.Payload().Mode == taskform.Edit {
		return []string{"task.edit"}
	}
	return []string{"task.create"}
}

func planNetworkProductSlugs(screen plannetwork.Screen, key string) []string {
	if key == "ctrl+s" && screen.Mode() == plannetwork.ModeGoal {
		return []string{"plan.edit"}
	}
	if key == "enter" && screen.Mode() == plannetwork.ModeAssign {
		return []string{"task.assign"}
	}
	return nil
}

func taskDetailContextualSlug(s taskdetail.Screen, key string) string {
	switch key {
	case "x":
		if s.Payload().Task.State == domain.TaskStateArchived {
			return "task.unarchive"
		}
		return "task.archive"
	case "enter":
		switch s.State().Mode {
		case taskdetail.ModeComment:
			return "comment.add"
		case taskdetail.ModeMove:
			return "task.transition"
		}
	case " ":
		if s.State().Focus == taskdetail.FocusSubtasks {
			return "task.transition"
		}
	}
	return ""
}

func productSlugsForHelpGroup(groupID, key string) []string {
	switch {
	case (groupID == "task_view" || groupID == "task_detail") && key == "b":
		return dependencyProductSlugs
	case groupID == "task_form" && key == "ctrl+b":
		return dependencyProductSlugs
	case groupID == "blocker_picker" && key == "ctrl+s":
		return dependencyProductSlugs
	}
	if slug := helpGroupProductSlugs[groupID][key]; slug != "" {
		return []string{slug}
	}
	return nil
}

func bindingKeyFragments(token string) []string {
	switch token {
	case "←/→", "← →":
		return []string{"left", "right"}
	case "↑/↓", "↑ ↓":
		return []string{"up", "down"}
	}
	token = strings.ReplaceAll(token, "·", "/")
	parts := strings.Split(token, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, " ") && !strings.Contains(part, "+") {
			out = append(out, strings.Fields(part)...)
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return []string{token}
	}
	return out
}
