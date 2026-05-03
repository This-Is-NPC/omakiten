package app

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/token"
)

type ContextService struct {
	tasks        TaskRepository
	comments     CommentRepository
	dependencies DependencyRepository
	entries      ContextEntryRepository
	config       ConfigRepository
	counter      token.Counter
}

func NewContextService(tasks TaskRepository, comments CommentRepository, dependencies DependencyRepository, entries ContextEntryRepository, config ConfigRepository, counter token.Counter) *ContextService {
	if counter == nil {
		counter = token.ApproxCounter{}
	}
	return &ContextService{tasks: tasks, comments: comments, dependencies: dependencies, entries: entries, config: config, counter: counter}
}

func (s *ContextService) Add(ctx context.Context, project domain.ProjectContext, body string) (domain.ContextEntry, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return domain.ContextEntry{}, domain.NewError(domain.ErrValidation, "context body is required", nil)
	}
	return s.entries.AddContextEntry(ctx, project.ID, body, s.counter.Count(body))
}

func (s *ContextService) Dump(ctx context.Context, project domain.ProjectContext, level int) (domain.ContextDump, error) {
	if level < 1 || level > 3 {
		return domain.ContextDump{}, domain.NewError(domain.ErrValidation, "context level must be 1, 2, or 3", nil)
	}
	settings, err := s.config.ContextSettings(ctx)
	if err != nil {
		return domain.ContextDump{}, err
	}
	budget := contextBudget{counter: s.counter, maxTokens: settings.MaxTokens}

	taskCount, err := s.tasks.TaskCount(ctx, project.ID)
	if err != nil {
		return domain.ContextDump{}, err
	}

	dump := domain.ContextDump{Project: project, Level: level, TaskCount: taskCount, TokenMetrics: domain.TokenMetrics{MaxTokens: settings.MaxTokens}}

	entries, err := s.entries.ListContextEntries(ctx, project.ID)
	if err != nil {
		return domain.ContextDump{}, err
	}
	for _, entry := range entries {
		estimate := entry.TokenEstimate
		if estimate <= 0 {
			estimate = s.counter.Count(entry.Body)
		}
		if !budget.add(estimate) {
			break
		}
		dump.ContextEntries = append(dump.ContextEntries, entry)
	}

	if level >= 2 {
		workflow, err := s.config.ActiveWorkflow(ctx)
		if err != nil {
			return domain.ContextDump{}, err
		}
		if budget.add(s.counter.Count(workflowText(workflow))) {
			dump.Workflow = workflow
		}

		tasks, err := s.tasks.ListTasks(ctx, project.ID, domain.TaskFilter{})
		if err != nil {
			return domain.ContextDump{}, err
		}
		for _, task := range tasks {
			if !budget.add(s.counter.Count(taskText(task))) {
				break
			}
			dump.Tasks = append(dump.Tasks, task)
		}

		dependencies, err := s.dependencies.ListTaskDependencies(ctx, project.ID, 0)
		if err != nil {
			return domain.ContextDump{}, err
		}
		for _, dependency := range dependencies {
			if !budget.add(s.counter.Count(fmt.Sprintf("%d depends on %d", dependency.TaskID, dependency.DependsOnTaskID))) {
				break
			}
			dump.Dependencies = append(dump.Dependencies, dependency)
		}
	}

	if level >= 3 {
		comments, err := s.comments.ListComments(ctx, project.ID, 0)
		if err != nil {
			return domain.ContextDump{}, err
		}
		for _, comment := range comments {
			if !budget.add(s.counter.Count(comment.Body)) {
				break
			}
			dump.Comments = append(dump.Comments, comment)
		}

		laws, err := s.config.ListActiveLaws(ctx)
		if err != nil {
			return domain.ContextDump{}, err
		}
		for _, law := range laws {
			if !budget.add(s.counter.Count(law.Key + " " + law.Severity + " " + law.Body)) {
				break
			}
			dump.Laws = append(dump.Laws, law)
		}
	}

	dump.TokenMetrics.EstimatedTotal = budget.total
	dump.TokenMetrics.Truncated = budget.truncated
	return dump, nil
}

type contextBudget struct {
	counter   token.Counter
	maxTokens int
	total     int
	truncated bool
}

func (b *contextBudget) add(estimate int) bool {
	if estimate < 0 {
		estimate = 0
	}
	if b.maxTokens > 0 && b.total+estimate > b.maxTokens {
		b.truncated = true
		return false
	}
	b.total += estimate
	return true
}

func taskText(task domain.Task) string {
	return strings.TrimSpace(task.Title + " " + task.Description + " " + task.BucketKey + " " + string(task.Priority))
}

func workflowText(workflow domain.Workflow) string {
	var b strings.Builder
	b.WriteString(workflow.Key)
	b.WriteByte(' ')
	b.WriteString(workflow.Name)
	for _, bucket := range workflow.Buckets {
		b.WriteByte(' ')
		b.WriteString(bucket.Key)
		b.WriteByte(' ')
		b.WriteString(bucket.Name)
	}
	for _, transition := range workflow.Transitions {
		b.WriteByte(' ')
		b.WriteString(transition.FromBucketKey)
		b.WriteString("->")
		b.WriteString(transition.ToBucketKey)
	}
	return b.String()
}
