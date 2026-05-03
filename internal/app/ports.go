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
}

type TaskRepository interface {
	CreateTask(ctx context.Context, projectID int64, title, description, bucketKey string) (domain.Task, error)
	ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter) ([]domain.Task, error)
	MoveTask(ctx context.Context, projectID, taskID int64, targetBucketKey string) (domain.Task, error)
	TaskCount(ctx context.Context, projectID int64) (int64, error)
}
