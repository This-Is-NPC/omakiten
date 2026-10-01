package httpapi

import (
	"context"
	"strings"

	"net/http"
	"omakiten/internal/contract"
)

// TaskOperations reads and changes the tasks of a project.
type TaskOperations interface {
	TaskBoard(ctx context.Context, input contract.TaskBoardInput) (contract.TaskBoardResponse, error)
	ListTasks(ctx context.Context, input contract.ListTasksInput) (contract.ListTasksResponse, error)
	ShowTask(ctx context.Context, input contract.ShowTaskInput) (contract.ShowTaskResponse, error)
	CreateTaskIntent(ctx context.Context, input contract.CreateTaskInput) (contract.CreateTaskResponse, error)
	EditTask(ctx context.Context, input contract.EditTaskInput) (contract.EditTaskResponse, error)
	MoveTask(ctx context.Context, input contract.MoveTaskInput) (contract.MoveTaskResponse, error)
	AssignTask(ctx context.Context, input contract.AssignTaskInput) (contract.AssignTaskResponse, error)
	ListTaskActivity(ctx context.Context, input contract.ListTaskActivityInput) (contract.ListTaskActivityResponse, error)
	DeleteTask(ctx context.Context, input contract.DeleteTaskInput) (contract.DeleteTaskResponse, error)
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

func (s *Server) taskRoutes() []route {
	return []route{
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
		query("deleteTask", http.MethodDelete, taskPath, "task.delete", "Hard-delete a task; without confirmation only the confirmation is returned.", []param{
			projectParam, taskParam,
			queryParam("confirmed", "`true` deletes; otherwise the confirmation is returned.", boolSchema),
		}, s.deleteTask),
		query("listTaskActivity", http.MethodGet, taskPath+"/activity", "task_activity.list", "Unified activity feed of a task.", []param{
			projectParam, taskParam,
			queryParam("order", "`asc` (default) or `desc`.", stringSchema),
		}, s.taskActivity),
	}
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

func (s *Server) deleteTask(r *http.Request) (contract.DeleteTaskResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.DeleteTaskResponse{}, err
	}
	confirmed, err := optionalBool(r, "confirmed")
	if err != nil {
		return contract.DeleteTaskResponse{}, err
	}
	return ops.DeleteTask(r.Context(), contract.DeleteTaskInput{ProjectSelector: selector, TaskID: taskID, Confirmed: confirmed != nil && *confirmed})
}

func (s *Server) taskActivity(r *http.Request) (contract.ListTaskActivityResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.ListTaskActivityResponse{}, err
	}
	return ops.ListTaskActivity(r.Context(), contract.ListTaskActivityInput{ProjectSelector: selector, TaskID: taskID, Order: r.URL.Query().Get("order")})
}
