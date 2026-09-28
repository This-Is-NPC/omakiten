package tui

import (
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/tui/components/screenbody"
	"omakiten/internal/tui/screenhost"
	projectscreen "omakiten/internal/tui/screens/project"
	"omakiten/internal/tui/screens/projectresume"
)

// openProjectView loads the project projection and pushes its explicit route.
func (m *Model) openProjectView() {
	m.status = ""
	m.projectScreen = m.boundProjectScreen().Loading(m.projectScreen.Generation() + 1)
	if err := m.refreshProjectSummary(); err != nil {
		m.status = err.Error()
		return
	}
	m.pushScreen(screenhost.Project)
}

func (m *Model) refreshProjectSummary() error {
	generation := m.projectScreen.Generation()
	payload, err := m.loadProjectPayload()
	m.projectScreen = m.boundProjectScreen().Apply(projectscreen.Result{Generation: generation, Payload: payload, Err: err})
	return err
}

func (m *Model) loadProjectPayload() (projectscreen.Payload, error) {
	payload := projectscreen.Payload{Project: m.project}
	events, err := m.commentsForProjectScope(domain.CommentFilter{})
	if err != nil {
		return payload, err
	}
	payload.Activity = events
	if m.repos.Projects != nil {
		if project, findErr := m.repos.Projects.FindProjectByID(m.ctx, m.project.ID); findErr == nil {
			payload.Description = project.Description
		}
	}
	if m.repos.Tags != nil {
		if tags, tagsErr := m.repos.Tags.ListProjectTags(m.ctx, m.project.ID); tagsErr == nil {
			payload.Tags = tags
		}
	}
	m.reloadProjectTasks()
	payload.Tasks = append([]domain.Task(nil), m.tasks...)
	payload.Dashboard = m.computeProjectDashboard()
	return payload, nil
}

func (m *Model) reloadProjectTasks() {
	if m.repos.Tasks == nil {
		return
	}
	views := m.activeViewSettings()
	tasks, err := m.repos.Tasks.ListTasks(m.ctx, m.project.ID, domain.TaskFilter{
		Sort: domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order}, IncludeArchived: m.includeArchived}, m.repos.activeSnapshot())
	if err != nil {
		return
	}
	m.tasks = tasks
	m.boardScreen = m.boundBoardScreen()
}

func (m *Model) computeProjectDashboard() projectscreen.Dashboard {
	data := projectscreen.Dashboard{}
	byBucket := map[string]int{}
	for _, task := range m.tasks {
		byBucket[task.BucketKey]++
		data.TotalTasks++
		if task.IsSubTask() {
			data.SubTasks++
		} else {
			data.RootTasks++
		}
	}
	for _, bucket := range m.workflow.Buckets {
		name := bucket.Name
		if name == "" {
			name = bucket.Key
		}
		data.Buckets = append(data.Buckets, projectscreen.BucketCount{Name: name, Count: byBucket[bucket.Key]})
	}
	if svc := m.repos.operationService(); svc != nil {
		if rollups, err := svc.ListPlanRollups(m.ctx, m.projectSelector()); err == nil {
			data.PlanCount = len(rollups)
			for _, rollup := range rollups {
				data.PlanDone += rollup.DoneCount
				data.PlanTotal += rollup.TotalCount
			}
		}
	}
	return data
}

func (m Model) boundProjectScreen() projectscreen.Screen {
	return m.projectScreen
}

func (m Model) boundProjectFormScreen() projectscreen.FormScreen {
	return m.projectFormReaderScreen
}

func (m *Model) openProjectResume() {
	m.status = ""
	m.projectResumeScreen = m.boundProjectResumeScreen().Loading()
	m.pushScreen(screenhost.ProjectResume)
	if err := m.refreshProjectResume(); err != nil {
		m.status = err.Error()
	}
}

func (m *Model) refreshProjectResume() error {
	payload, err := m.loadProjectResumePayload()
	screen := m.boundProjectResumeScreen().Apply(payload)
	m.projectResumeScreen = screenbody.AfterApply(screen, m.screenFrame()).(projectresume.Screen)
	return err
}

func (m *Model) loadProjectResumePayload() (projectresume.Payload, error) {
	svc := m.repos.operationService()
	if svc == nil {
		return projectresume.Payload{Err: errResumeUnavailable{msg: m.t("tui.status.resume_unavailable")}}, errResumeUnavailable{msg: m.t("tui.status.resume_unavailable")}
	}
	resp, err := svc.ResumeProject(m.ctx, operation.ResumeProjectInput{
		ProjectSelector: operation.ProjectSelector{ProjectID: m.project.ID}})
	if err != nil {
		return projectresume.Payload{Err: err}, err
	}
	return projectresume.Payload{
		TaskBuckets:    resumeBuckets(resp.TaskBuckets),
		LikelyNextWork: resumeTasks(resp.LikelyNextWork),
		BlockedWork:    resumeTasks(resp.BlockedWork),
		Dependencies:   resumeDeps(resp.Dependencies),
		NextStepPrompt: resp.NextStepPrompt}, nil
}

func resumeBuckets(in []operation.BucketCount) []projectresume.BucketCount {
	out := make([]projectresume.BucketCount, len(in))
	for i, b := range in {
		out[i] = projectresume.BucketCount{BucketKey: b.BucketKey, Name: b.Name, Count: b.Count}
	}
	return out
}

func resumeTasks(in []operation.TaskSummary) []projectresume.TaskSummary {
	out := make([]projectresume.TaskSummary, len(in))
	for i, t := range in {
		out[i] = projectresume.TaskSummary{ID: t.ID, Title: t.Title, BucketKey: t.BucketKey, Priority: t.Priority}
	}
	return out
}

func resumeDeps(in []operation.DependencySummary) []projectresume.DependencySummary {
	out := make([]projectresume.DependencySummary, len(in))
	for i, d := range in {
		out[i] = projectresume.DependencySummary{TaskID: d.TaskID, DependsOnTaskID: d.DependsOnTaskID}
	}
	return out
}

type errResumeUnavailable struct{ msg string }

func (e errResumeUnavailable) Error() string { return e.msg }

func (m Model) boundProjectResumeScreen() projectresume.Screen {
	return m.projectResumeScreen
}
