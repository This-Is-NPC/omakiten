package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

const (
	projectsPath = "/api/v1/projects"
	projectPath  = projectsPath + "/{project}"
	taskPath     = projectPath + "/tasks/{task}"
)

var (
	projectParam = pathParam("project", "Project slug.", stringSchema)
	taskParam    = pathParam("task", "Task id.", idSchema)
)

// HealthResponse reports daemon liveness.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// CatalogResponse carries every catalog entry for the GUI language.
type CatalogResponse struct {
	Language string            `json:"language"`
	Entries  map[string]string `json:"entries"`
}

// CreateTaskBody is the createTask request body.
type CreateTaskBody struct {
	Title        string `json:"title,omitempty"`
	Description  string `json:"description"`
	Priority     string `json:"priority,omitempty"`
	BucketKey    string `json:"bucket_key,omitempty"`
	TemplateSlug string `json:"template_slug,omitempty"`
	ParentID     *int64 `json:"parent_id,omitempty"`
	// Confirmed creates the task even when similar work exists.
	Confirmed bool `json:"confirmed,omitempty"`
}

// EditTaskBody is the editTask request body; absent fields stay unchanged.
type EditTaskBody struct {
	Title       *string                `json:"title,omitempty"`
	Description *string                `json:"description,omitempty"`
	Priority    *string                `json:"priority,omitempty"`
	ParentID    contract.OptionalInt64 `json:"parent_id,omitempty"`
}

// TransitionBody names the target bucket of moveTask.
type TransitionBody struct {
	BucketKey string `json:"bucket_key"`
}

// AssigneeBody sets the assignee; empty clears it.
type AssigneeBody struct {
	Assignee string `json:"assignee"`
}

// CommentBody is the addTaskComment request body.
type CommentBody struct {
	Body         string   `json:"body"`
	Title        string   `json:"title,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Pinned       bool     `json:"pinned,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	TemplateSlug string   `json:"template_slug,omitempty"`
}

func (s *Server) routes() []route {
	return []route{
		s.healthRoute(),
		query("getCatalog", http.MethodGet, "/api/v1/catalog", "", "Text for the GUI language preference.", nil, s.catalog),
		query("listProjects", http.MethodGet, projectsPath, "project.list", "Registered projects.", nil, s.listProjects),
		query("getProject", http.MethodGet, projectPath, "project.overview", "Project overview with bucket counts.", []param{projectParam}, s.projectOverview),
		query("resumeProject", http.MethodGet, projectPath+"/resume", "project.resume", "Context to resume work on a project.", []param{projectParam}, s.resumeProject),
		query("getWorkflow", http.MethodGet, projectPath+"/workflow", "workflow.show", "Buckets, transitions, and permissions.", []param{projectParam}, s.workflow),
		query("getBoard", http.MethodGet, projectPath+"/board", "task.list", "Workflow and active tasks with their relation counts.", []param{projectParam}, s.board),
		query("listTasks", http.MethodGet, projectPath+"/tasks", "task.list", "Tasks of a project.", []param{
			projectParam,
			queryParam("bucket", "Bucket key filter.", stringSchema),
			queryParam("parent", "`root` for root tasks or a parent task id.", stringSchema),
		}, s.listTasks),
		command("createTask", http.MethodPost, projectPath+"/tasks", "task.create_intent", "Create a task; similar work requires confirmation.", []param{projectParam}, s.createTask),
		query("getTask", http.MethodGet, taskPath, "task.show", "A task with dependencies and every comment.", []param{projectParam, taskParam}, s.showTask),
		command("editTask", http.MethodPatch, taskPath, "task.edit", "Edit task fields.", []param{projectParam, taskParam}, s.editTask),
		command("moveTask", http.MethodPost, taskPath+"/transitions", "task.transition", "Move a task through a workflow transition.", []param{projectParam, taskParam}, s.moveTask),
		command("assignTask", http.MethodPut, taskPath+"/assignee", "task.assign", "Set or clear the task assignee.", []param{projectParam, taskParam}, s.assignTask),
		query("listTaskComments", http.MethodGet, taskPath+"/comments", "comment.list", "Comments of a task.", []param{projectParam, taskParam}, s.listComments),
		command("addTaskComment", http.MethodPost, taskPath+"/comments", "comment.add", "Add a human comment to a task.", []param{projectParam, taskParam}, s.addComment),
		query("listTaskActivity", http.MethodGet, taskPath+"/activity", "task_activity.list", "Unified activity feed of a task.", []param{
			projectParam, taskParam,
			queryParam("order", "`asc` (default) or `desc`.", stringSchema),
		}, s.taskActivity),
		query("listDependencies", http.MethodGet, projectPath+"/dependencies", "dependency.list", "Task dependencies of a project.", []param{
			projectParam,
			queryParam("task", "Limit to one task id.", idSchema),
		}, s.listDependencies),
		query("listPlans", http.MethodGet, projectPath+"/plans", "plan.list", "Plans of a project.", []param{projectParam}, s.listPlans),
		query("getPlan", http.MethodGet, projectPath+"/plans/{plan}", "plan.show", "A plan with its waves and tasks.", []param{
			projectParam,
			pathParam("plan", "Plan slug.", stringSchema),
		}, s.showPlan),
		query("search", http.MethodGet, projectPath+"/search", "search", "Full-text search within a project.", []param{
			projectParam,
			queryParam("q", "Search query.", stringSchema),
			queryParam("type", "Entity type filter; repeatable or comma-separated.", listSchema),
		}, s.search),
		query("listLogs", http.MethodGet, projectPath+"/logs", "logs.list", "Event log of a project.", []param{
			projectParam,
			queryParam("category", "Event category; repeatable or comma-separated.", listSchema),
			queryParam("since", "Window such as `24h` or `7d`.", stringSchema),
			queryParam("limit", "Row cap.", intSchema),
			queryParam("order", "`asc` or `desc` (default).", stringSchema),
		}, s.listLogs),
		query("getInsights", http.MethodGet, projectPath+"/insights", "insights.summary", "Today's insights for a project.", []param{
			projectParam,
			queryParam("stuck_days", "Days without movement that mark a task stuck.", intSchema),
		}, s.insights),
		query("getMetrics", http.MethodGet, projectPath+"/metrics", "metrics.summary", "Task and event metrics for a project.", []param{
			projectParam,
			queryParam("period", "Window such as `7d`.", stringSchema),
		}, s.metrics),
		s.eventsRoute(),
	}
}

func (s *Server) healthRoute() route {
	r := query("getHealth", http.MethodGet, "/health", "", "Daemon liveness; needs no token.", nil, func(*http.Request) (HealthResponse, error) {
		return HealthResponse{Status: "ok", Version: s.opts.Version}, nil
	})
	r.public = true
	return r
}

func (s *Server) catalog(*http.Request) (CatalogResponse, error) {
	catalog := s.opts.Runtimes.Catalog()
	return CatalogResponse{Language: catalog.Code(), Entries: catalog.Entries()}, nil
}

func (s *Server) listProjects(r *http.Request) ([]domain.Project, error) {
	ops, err := s.opts.Runtimes.Global(r.Context())
	if err != nil {
		return nil, err
	}
	return ops.ListProjects(r.Context())
}

func (s *Server) projectOverview(r *http.Request) (contract.OverviewResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.OverviewResponse{}, err
	}
	return ops.Overview(r.Context(), contract.OverviewInput{ProjectSelector: selector})
}

func (s *Server) resumeProject(r *http.Request) (contract.ResumeProjectResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ResumeProjectResponse{}, err
	}
	return ops.ResumeProject(r.Context(), contract.ResumeProjectInput{ProjectSelector: selector})
}

func (s *Server) workflow(r *http.Request) (contract.WorkflowResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.WorkflowResponse{}, err
	}
	return ops.ShowWorkflow(r.Context(), contract.WorkflowInput{ProjectSelector: selector})
}

func (s *Server) board(r *http.Request) (contract.TaskBoardResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.TaskBoardResponse{}, err
	}
	return ops.TaskBoard(r.Context(), contract.TaskBoardInput{ProjectSelector: selector})
}

func (s *Server) listTasks(r *http.Request) (contract.ListTasksResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ListTasksResponse{}, err
	}
	input := contract.ListTasksInput{ProjectSelector: selector, BucketKey: r.URL.Query().Get("bucket")}
	switch parent := strings.TrimSpace(r.URL.Query().Get("parent")); parent {
	case "":
	case "root":
		input.ParentID = contract.OptionalInt64{Set: true}
	default:
		id, err := parseID(parent, "parent")
		if err != nil {
			return contract.ListTasksResponse{}, err
		}
		input.ParentID = contract.OptionalInt64{Set: true, Value: &id}
	}
	return ops.ListTasks(r.Context(), input)
}

func (s *Server) createTask(r *http.Request, body CreateTaskBody) (contract.CreateTaskResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.CreateTaskResponse{}, err
	}
	return ops.CreateTaskIntent(r.Context(), contract.CreateTaskInput{
		ProjectSelector: selector,
		Title:           body.Title,
		Description:     body.Description,
		Priority:        body.Priority,
		BucketKey:       body.BucketKey,
		TemplateSlug:    body.TemplateSlug,
		ParentID:        body.ParentID,
		Confirmed:       body.Confirmed,
	})
}

func (s *Server) showTask(r *http.Request) (contract.ShowTaskResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.ShowTaskResponse{}, err
	}
	return ops.ShowTask(r.Context(), contract.ShowTaskInput{ProjectSelector: selector, TaskID: taskID})
}

func (s *Server) editTask(r *http.Request, body EditTaskBody) (contract.EditTaskResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.EditTaskResponse{}, err
	}
	return ops.EditTask(r.Context(), contract.EditTaskInput{
		ProjectSelector: selector,
		TaskID:          taskID,
		Title:           body.Title,
		Description:     body.Description,
		Priority:        body.Priority,
		ParentID:        body.ParentID,
	})
}

func (s *Server) moveTask(r *http.Request, body TransitionBody) (contract.MoveTaskResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.MoveTaskResponse{}, err
	}
	return ops.MoveTask(r.Context(), contract.MoveTaskInput{ProjectSelector: selector, TaskID: taskID, BucketKey: body.BucketKey})
}

func (s *Server) assignTask(r *http.Request, body AssigneeBody) (contract.AssignTaskResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.AssignTaskResponse{}, err
	}
	return ops.AssignTask(r.Context(), contract.AssignTaskInput{ProjectSelector: selector, TaskID: taskID, Assignee: body.Assignee})
}

func (s *Server) listComments(r *http.Request) (contract.CommentsResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.CommentsResponse{}, err
	}
	return ops.ListComments(r.Context(), contract.ListCommentsInput{ProjectSelector: selector, TaskID: taskID})
}

func (s *Server) addComment(r *http.Request, body CommentBody) (contract.CommentResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.CommentResponse{}, err
	}
	return ops.AddComment(r.Context(), contract.AddCommentInput{
		ProjectSelector: selector,
		TaskID:          taskID,
		Body:            body.Body,
		Title:           body.Title,
		Kind:            body.Kind,
		Pinned:          body.Pinned,
		AuthorType:      "human",
		Tags:            body.Tags,
		TemplateSlug:    body.TemplateSlug,
	})
}

func (s *Server) taskActivity(r *http.Request) (contract.ListTaskActivityResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.ListTaskActivityResponse{}, err
	}
	return ops.ListTaskActivity(r.Context(), contract.ListTaskActivityInput{ProjectSelector: selector, TaskID: taskID, Order: r.URL.Query().Get("order")})
}

func (s *Server) listDependencies(r *http.Request) (contract.DependenciesResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.DependenciesResponse{}, err
	}
	input := contract.ListDependenciesInput{ProjectSelector: selector}
	if raw := r.URL.Query().Get("task"); raw != "" {
		if input.TaskID, err = parseID(raw, "task"); err != nil {
			return contract.DependenciesResponse{}, err
		}
	}
	return ops.ListDependencies(r.Context(), input)
}

func (s *Server) listPlans(r *http.Request) (contract.ListPlansResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ListPlansResponse{}, err
	}
	return ops.ListPlans(r.Context(), contract.ListPlansInput{ProjectSelector: selector})
}

func (s *Server) showPlan(r *http.Request) (contract.ShowPlanResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ShowPlanResponse{}, err
	}
	return ops.ShowPlan(r.Context(), contract.ShowPlanInput{ProjectSelector: selector, Slug: r.PathValue("plan")})
}

func (s *Server) search(r *http.Request) (contract.SearchResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.SearchResponse{}, err
	}
	return ops.Search(r.Context(), contract.SearchInput{ProjectSelector: selector, Query: r.URL.Query().Get("q"), EntityTypes: listValues(r, "type")})
}

func (s *Server) listLogs(r *http.Request) (contract.ListLogsResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ListLogsResponse{}, err
	}
	limit, err := optionalInt(r, "limit")
	if err != nil {
		return contract.ListLogsResponse{}, err
	}
	return ops.ListLogs(r.Context(), contract.ListLogsInput{
		ProjectSelector: selector,
		Categories:      listValues(r, "category"),
		Since:           r.URL.Query().Get("since"),
		Limit:           limit,
		Order:           r.URL.Query().Get("order"),
	})
}

func (s *Server) insights(r *http.Request) (contract.InsightsSummaryResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.InsightsSummaryResponse{}, err
	}
	stuckDays, err := optionalInt(r, "stuck_days")
	if err != nil {
		return contract.InsightsSummaryResponse{}, err
	}
	return ops.InsightsSummary(r.Context(), contract.InsightsSummaryInput{ProjectSelector: selector, StuckDays: stuckDays})
}

func (s *Server) metrics(r *http.Request) (contract.MetricsSummaryResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.MetricsSummaryResponse{}, err
	}
	return ops.MetricsSummary(r.Context(), contract.MetricsSummaryInput{ProjectSelector: selector, Period: r.URL.Query().Get("period"), ProjectID: selector.ProjectID})
}

func (s *Server) project(r *http.Request) (Operations, contract.ProjectSelector, error) {
	return s.opts.Runtimes.Project(r.Context(), r.PathValue("project"))
}

func (s *Server) task(r *http.Request) (Operations, contract.ProjectSelector, int64, error) {
	taskID, err := parseID(r.PathValue("task"), "task")
	if err != nil {
		return nil, contract.ProjectSelector{}, 0, err
	}
	ops, selector, err := s.project(r)
	return ops, selector, taskID, err
}

func parseID(raw, name string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, invalidParameter(name, raw)
	}
	return id, nil
}

func optionalInt(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalidParameter(name, raw)
	}
	return value, nil
}

// listValues accepts repeated and comma-separated query values.
func listValues(r *http.Request, name string) []string {
	var out []string
	for _, raw := range r.URL.Query()[name] {
		for value := range strings.SplitSeq(raw, ",") {
			if value = strings.TrimSpace(value); value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}
