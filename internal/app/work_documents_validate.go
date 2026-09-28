package app

import (
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/graph"
)

// ValidateWorkDocument checks references and cycles before any persistence.
func ValidateWorkDocument(doc domain.WorkDocument) error {
	if doc.Spec.Version != 1 {
		return documentError("unsupported omakiten.version; expected 1")
	}
	if strings.TrimSpace(doc.Title) == "" {
		return documentError("document title is required")
	}
	if err := validateWorkType(doc); err != nil {
		return err
	}
	if err := validateWorkWaves(doc.Spec.Waves); err != nil {
		return err
	}
	tasks := doc.TaskList()
	if err := validateWorkTasks(tasks); err != nil {
		return err
	}
	return validateWorkMembership(doc, tasks)
}

func validateWorkType(doc domain.WorkDocument) error {
	switch doc.Type {
	case "Omakiten Plan":
		if doc.Spec.Slug == "" || doc.Spec.Task != nil {
			return documentError("plan requires a slug and no standalone task")
		}
		if err := domain.ValidatePlanGoalBody(doc.Body); err != nil {
			return err
		}
		if doc.Spec.Status != "" && doc.Spec.Status != "active" && doc.Spec.Status != "done" && doc.Spec.Status != "abandoned" {
			return documentError("invalid plan status")
		}
	case "Omakiten Task":
		if doc.Spec.Task == nil || len(doc.Spec.Waves) != 0 || doc.Spec.Slug != "" || doc.Spec.Status != "" {
			return documentError("task document requires omakiten.task and no plan fields")
		}
	default:
		return documentError("expected type: Omakiten Plan or Omakiten Task")
	}
	return nil
}

func validateWorkWaves(waves []domain.WorkWave) error {
	seen := map[string]bool{}
	for _, wave := range waves {
		if wave.Key == "" || seen[wave.Key] || strings.TrimSpace(wave.Name) == "" {
			return documentError("waves require unique keys and nonempty names")
		}
		seen[wave.Key] = true
	}
	return nil
}

func validateWorkTasks(tasks []domain.WorkTask) error {
	keys := map[string]int64{}
	for i, task := range tasks {
		if task.Key == "" || keys[task.Key] != 0 {
			return documentError("tasks require unique nonempty keys")
		}
		keys[task.Key] = int64(i + 1)
		if err := validateWorkTask(task); err != nil {
			return err
		}
	}
	for _, parents := range []bool{false, true} {
		edges, err := workReferenceEdges(tasks, keys, parents)
		if err != nil {
			return err
		}
		if graph.HasCycle(edges) {
			return documentError("document contains a dependency or parent cycle")
		}
	}
	return nil
}

func validateWorkTask(task domain.WorkTask) error {
	if err := domain.ValidateTaskTitle(task.Title); err != nil {
		return err
	}
	if strings.TrimSpace(task.Title) == "" {
		return documentError("task title is required")
	}
	if err := domain.ValidateTaskDescription(task.Description); err != nil {
		return err
	}
	if task.State != "" && task.State != "active" && task.State != "archived" {
		return documentError("invalid task state")
	}
	return nil
}

func taskReferences(task domain.WorkTask, parents bool) []string {
	if !parents {
		return task.DependsOn
	}
	if task.Parent == "" {
		return nil
	}
	return []string{task.Parent}
}

func workReferenceEdges(tasks []domain.WorkTask, keys map[string]int64, parents bool) ([]graph.Edge, error) {
	var edges []graph.Edge
	for _, task := range tasks {
		seen := map[string]bool{}
		for _, key := range taskReferences(task, parents) {
			if keys[key] == 0 || seen[key] {
				return nil, documentError("unknown or duplicate task reference: " + key)
			}
			seen[key] = true
			edges = append(edges, graph.Edge{From: keys[task.Key], To: keys[key]})
		}
	}
	return edges, nil
}

func validateWorkMembership(doc domain.WorkDocument, tasks []domain.WorkTask) error {
	members, parents, err := workMembership(doc, tasks)
	if err != nil {
		return err
	}
	for _, wave := range doc.Spec.Waves {
		for _, task := range wave.Tasks {
			if !members[task.Key] {
				return documentError("wave tasks must belong to the plan")
			}
		}
	}
	for _, task := range tasks {
		key := task.Key
		for key != "" && !members[key] {
			key = parents[key]
		}
		if key == "" {
			return documentError("every task must belong to the document's parent tree or plan")
		}
	}
	return nil
}

func workMembership(doc domain.WorkDocument, tasks []domain.WorkTask) (map[string]bool, map[string]string, error) {
	members := map[string]bool{}
	parents := map[string]string{}
	for _, task := range tasks {
		parents[task.Key] = task.Parent
		if doc.Spec.Task != nil {
			if task.PlanMember != nil {
				return nil, nil, documentError("plan_member belongs only in plan documents")
			}
			members[task.Key] = task.Key == doc.Spec.Task.Key
		} else {
			members[task.Key] = task.PlanMember == nil || *task.PlanMember
		}
	}
	if doc.Spec.Task != nil && doc.Spec.Task.Parent != "" {
		return nil, nil, documentError("the document root task cannot have a parent")
	}
	return members, parents, nil
}
