package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"omakiten/internal/activity"
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
	registry     *domain.EnumRegistry
}

func NewContextService(tasks TaskRepository, comments CommentRepository, dependencies DependencyRepository, entries ContextEntryRepository, cfg ConfigRepository, counter token.Counter, registry *domain.EnumRegistry) *ContextService {
	if counter == nil {
		counter = token.ApproxCounter{}
	}
	return &ContextService{tasks: tasks, comments: comments, dependencies: dependencies, entries: entries, config: cfg, counter: counter, registry: registry}
}

func (s *ContextService) Add(ctx context.Context, project domain.ProjectContext, body string) (entry domain.ContextEntry, err error) {
	finish := activity.Track(ctx, "app.ContextService.Add", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	body = strings.TrimSpace(body)
	if body == "" {
		err = domain.NewError(domain.ErrValidation, "context body is required", nil)
		return
	}
	entry, err = s.entries.AddContextEntry(ctx, project.ID, body, s.counter.Count(body))
	return
}

func (s *ContextService) Dump(ctx context.Context, project domain.ProjectContext, level int) (dump domain.ContextDump, err error) {
	finish := activity.Track(ctx, "app.ContextService.Dump", project, map[string]any{"level": level})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if level < 1 || level > 3 {
		err = domain.NewError(domain.ErrValidation, "context level must be 1, 2, or 3", nil)
		return
	}
	snap := s.config.Snapshot()
	settings := snap.ContextSettings()
	budget := contextBudget{counter: s.counter, maxTokens: settings.MaxTokens}

	taskCount, err := s.tasks.TaskCount(ctx, project.ID)
	if err != nil {
		return
	}

	dump = domain.ContextDump{Project: project, Level: level, TaskCount: taskCount, TokenMetrics: domain.TokenMetrics{MaxTokens: settings.MaxTokens}}

	entries, err := s.entries.ListContextEntries(ctx, project.ID)
	if err != nil {
		return
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
		workflow := snap.Workflow()
		if budget.add(s.counter.Count(workflowText(workflow))) {
			dump.Workflow = workflow
		}

		tasks, err := s.tasks.ListTasks(ctx, project.ID, domain.TaskFilter{})
		if err != nil {
			return dump, err
		}
		for _, task := range tasks {
			if !budget.add(s.counter.Count(s.taskText(task))) {
				break
			}
			dump.Tasks = append(dump.Tasks, task)
		}

		dependencies, err := s.dependencies.ListTaskDependencies(ctx, project.ID, 0)
		if err != nil {
			return dump, err
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
			return dump, err
		}
		for _, comment := range comments {
			if !budget.add(s.counter.Count(comment.Body)) {
				break
			}
			dump.Comments = append(dump.Comments, comment)
		}

		for _, law := range lawsFromSnapshot(snap) {
			if !budget.add(s.counter.Count(law.Key + " " + s.severityText(law.Severity) + " " + law.Body)) {
				break
			}
			dump.Laws = append(dump.Laws, law)
		}
	}

	dump.TokenMetrics.EstimatedTotal = budget.total
	dump.TokenMetrics.Truncated = budget.truncated
	return
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

func (s *ContextService) taskText(task domain.Task) string {
	// Resolve the priority label through the bundle-scoped registry so
	// token estimation reflects the actual string the renderer ships
	// ("low" 3 chars vs "high" 4 chars), regardless of user customisation
	// to config.priorities. Unknown id falls back to the numeric handle.
	label := s.registry.PriorityLabel(task.Priority)
	if label == "" {
		label = strconv.Itoa(int(task.Priority))
	}
	return strings.TrimSpace(task.Title + " " + task.Description + " " + task.BucketKey + " " + label)
}

// severityText resolves the severity label through the bundle-scoped
// registry, falling back to the numeric id when the entry has been
// removed from config since the law was loaded.
func (s *ContextService) severityText(sev domain.Severity) string {
	if label := s.registry.SeverityLabel(sev); label != "" {
		return label
	}
	return strconv.Itoa(int(sev))
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
