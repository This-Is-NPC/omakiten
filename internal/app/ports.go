package app

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type ProjectRepository interface {
	UpsertProject(ctx context.Context, name, slug, rootPath string) (domain.Project, error)
	FindProjectByID(ctx context.Context, id int64) (domain.Project, error)
	FindProjectBySlug(ctx context.Context, slug string) (domain.Project, error)
	FindProjectsContainingPath(ctx context.Context, path string) ([]domain.Project, error)
}

type ConfigRepository interface {
	ImportBundle(ctx context.Context, bundle config.Bundle, sourcePath, sourceHash string) error
	ListActiveLaws(ctx context.Context) ([]domain.Law, error)
	ListActiveSkills(ctx context.Context) ([]domain.Skill, error)
	ListActivePersonas(ctx context.Context) ([]domain.Persona, error)
	ActiveWorkflow(ctx context.Context) (domain.Workflow, error)
	ContextSettings(ctx context.Context) (domain.ContextSettings, error)
}

type TaskRepository interface {
	CreateTask(ctx context.Context, projectID int64, title, description, bucketKey string) (domain.Task, error)
	ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter) ([]domain.Task, error)
	MoveTask(ctx context.Context, projectID, taskID int64, targetBucketKey string) (domain.Task, error)
	UpdateTask(ctx context.Context, projectID, taskID int64, update domain.TaskUpdate) (domain.Task, error)
	TaskCount(ctx context.Context, projectID int64) (int64, error)
}

type CommentRepository interface {
	AddComment(ctx context.Context, projectID, taskID int64, body, authorType string) (domain.Comment, error)
	ListComments(ctx context.Context, projectID, taskID int64) ([]domain.Comment, error)
}

type DependencyRepository interface {
	AddTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) (domain.TaskDependency, error)
	RemoveTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) error
	ListTaskDependencies(ctx context.Context, projectID, taskID int64) ([]domain.TaskDependency, error)
}

type ContextEntryRepository interface {
	AddContextEntry(ctx context.Context, projectID int64, body string, tokenEstimate int) (domain.ContextEntry, error)
	ListContextEntries(ctx context.Context, projectID int64) ([]domain.ContextEntry, error)
}
