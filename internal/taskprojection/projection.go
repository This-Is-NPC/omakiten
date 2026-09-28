package taskprojection

import (
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// Input is the complete snapshot needed to build a task projection.
type Input struct {
	Tasks        []domain.Task
	Workflow     domain.Workflow
	Dependencies []domain.TaskDependency
	Comments     []domain.Comment
	Priorities   []config.PriorityDefinition
}

// Query describes a view-specific task selection. Relation and count data are
// shared regardless of the query; only the visible task slice changes.
type Query struct {
	RootOnly  bool
	Priority  []string
	Bucket    []string
	SortField string
	SortOrder string
}

// BadgeCounts contains the relation counts painted on a task card or row.
type BadgeCounts struct {
	Blockers int
	Comments int
	Subtasks int
}

// ChildLane is one workflow lane of direct children in deterministic order.
type ChildLane struct {
	Bucket domain.Bucket
	Tasks  []domain.Task
}

// ChildLaneSet is the prepared child-board projection for one parent.
type ChildLaneSet struct {
	Lanes     []ChildLane
	Completed int
	Total     int
}

// Projection is the immutable task/dependency read model for task screens.
// Slices returned by its methods are copies, so screen-local cursor state
// cannot mutate the host snapshot or another screen's view.
type Projection struct {
	tasks       []domain.Task
	byID        map[int64]domain.Task
	children    map[int64][]domain.Task
	childLanes  map[int64]ChildLaneSet
	blockers    map[int64][]domain.Task
	ancestors   map[int64][]int64
	badges      map[int64]BadgeCounts
	buckets     []domain.Bucket
	priorities  []config.PriorityDefinition
	comments    []domain.Comment
	finalBucket string
	fingerprint uint64
}

// Build creates a deterministic projection from an in-memory snapshot.
func Build(input Input) Projection {
	tasks := append([]domain.Task(nil), input.Tasks...)
	sortProjectionTasks(tasks)
	p := newProjection(tasks, input)
	p.indexTasks()
	p.indexDependencies(input.Dependencies)
	p.indexComments(input.Comments)
	p.finishRelations()
	p.fingerprint = fingerprint(p)
	return p
}

func newProjection(tasks []domain.Task, input Input) Projection {
	return Projection{
		tasks:       tasks,
		byID:        make(map[int64]domain.Task, len(tasks)),
		children:    make(map[int64][]domain.Task),
		childLanes:  make(map[int64]ChildLaneSet),
		blockers:    make(map[int64][]domain.Task),
		ancestors:   make(map[int64][]int64),
		badges:      make(map[int64]BadgeCounts),
		buckets:     append([]domain.Bucket(nil), input.Workflow.Buckets...),
		priorities:  append([]config.PriorityDefinition(nil), input.Priorities...),
		comments:    append([]domain.Comment(nil), input.Comments...),
		finalBucket: input.Workflow.FinalBucketKey(),
	}
}

func (p *Projection) indexTasks() {
	for _, task := range p.tasks {
		p.byID[task.ID] = task
		if task.ParentID == nil {
			continue
		}
		parentID := *task.ParentID
		p.children[parentID] = append(p.children[parentID], task)
	}
}

func (p *Projection) indexDependencies(dependencies []domain.TaskDependency) {
	for _, dependency := range dependencies {
		blocker, ok := p.byID[dependency.DependsOnTaskID]
		if !ok {
			continue
		}
		p.blockers[dependency.TaskID] = append(p.blockers[dependency.TaskID], blocker)
		counts := p.badges[dependency.TaskID]
		counts.Blockers++
		p.badges[dependency.TaskID] = counts
	}
}

func (p *Projection) indexComments(comments []domain.Comment) {
	for _, comment := range comments {
		counts := p.badges[comment.TaskID]
		counts.Comments++
		p.badges[comment.TaskID] = counts
	}
}

func (p *Projection) finishRelations() {
	for parentID, children := range p.children {
		counts := p.badges[parentID]
		counts.Subtasks = len(children)
		p.badges[parentID] = counts
		sortTasks(children)
		p.children[parentID] = children
		p.childLanes[parentID] = buildChildLanes(p.buckets, children, p.finalBucket)
	}
	for taskID, blockers := range p.blockers {
		sortTasks(blockers)
		p.blockers[taskID] = blockers
	}
	for _, task := range p.tasks {
		p.ancestors[task.ID] = p.parentChain(task.ID)
	}
}

// Rebuild applies refreshed task or dependency rows while retaining the
// projection's workflow, comments and priority configuration.
func (p Projection) Rebuild(tasks []domain.Task, dependencies []domain.TaskDependency) Projection {
	return Build(Input{
		Tasks:        tasks,
		Workflow:     domain.Workflow{Buckets: p.Buckets()},
		Dependencies: dependencies,
		Comments:     p.comments,
		Priorities:   append([]config.PriorityDefinition(nil), p.priorities...),
	})
}

// Tasks returns tasks matching query semantics. RootOnly excludes subtasks;
// an empty filter means all matching tasks. Ordering is always deterministic.
func (p Projection) Tasks(query Query) []domain.Task {
	prioritySet := stringSet(query.Priority)
	bucketSet := stringSet(query.Bucket)
	rows := make([]domain.Task, 0, len(p.tasks))
	for _, task := range p.tasks {
		if query.RootOnly && task.IsSubTask() {
			continue
		}
		if !allows(bucketSet, task.BucketKey) || !p.allowsPriority(prioritySet, task.Priority) {
			continue
		}
		rows = append(rows, task)
	}
	if query.SortField == "" {
		return rows
	}
	asc := query.SortOrder != "desc"
	sort.SliceStable(rows, func(i, j int) bool {
		if lessTask(rows[i], rows[j], query.SortField) {
			return asc
		}
		if lessTask(rows[j], rows[i], query.SortField) {
			return !asc
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

// RootTasksByBucket returns root tasks grouped under the supplied workflow
// buckets. Empty lanes are included so the board can paint its full workflow.
func (p Projection) RootTasksByBucket(query Query) map[string][]domain.Task {
	query.RootOnly = true
	rows := p.Tasks(query)
	grouped := make(map[string][]domain.Task, len(p.buckets))
	for _, bucket := range p.buckets {
		grouped[bucket.Key] = nil
	}
	for _, task := range rows {
		grouped[task.BucketKey] = append(grouped[task.BucketKey], task)
	}
	return grouped
}

// Children returns direct children in deterministic order.
func (p Projection) Children(parentID int64) []domain.Task { return cloneTasks(p.children[parentID]) }

// ChildLanes returns the ordered, pre-grouped direct children for a parent.
// The result includes completion counts based on the workflow's final bucket.
// Returned slices are copies so screen-local cursor state cannot mutate the
// projection.
func (p Projection) ChildLanes(parentID int64) ChildLaneSet {
	set, ok := p.childLanes[parentID]
	if !ok {
		set = buildChildLanes(p.buckets, nil, p.finalBucket)
	}
	return cloneChildLaneSet(set)
}

// Blockers returns the tasks this task directly depends on.
func (p Projection) Blockers(taskID int64) []domain.Task { return cloneTasks(p.blockers[taskID]) }

// Candidates returns deterministic blocker-picker candidates excluding the
// current task itself.
func (p Projection) Candidates(taskID int64) []domain.Task {
	rows := make([]domain.Task, 0, max(0, len(p.tasks)-1))
	for _, task := range p.tasks {
		if task.ID != taskID {
			rows = append(rows, task)
		}
	}
	return rows
}

// Ancestors returns the parent chain from the immediate parent toward the root.
func (p Projection) Ancestors(taskID int64) []int64 {
	return append([]int64(nil), p.ancestors[taskID]...)
}

// TaskByID looks up a task in the projection.
func (p Projection) TaskByID(taskID int64) (domain.Task, bool) {
	task, ok := p.byID[taskID]
	return task, ok
}

// Badges returns relation counts for a task.
func (p Projection) Badges(taskID int64) BadgeCounts { return p.badges[taskID] }

// IsComplete classifies completion using the workflow's final bucket.
func (p Projection) IsComplete(taskID int64) bool {
	task, ok := p.byID[taskID]
	return ok && p.finalBucket != "" && task.BucketKey == p.finalBucket
}

// Completion returns the completed and total direct-child counts.
func (p Projection) Completion(parentID int64) (done, total int) {
	children := p.children[parentID]
	for _, child := range children {
		if p.IsComplete(child.ID) {
			done++
		}
	}
	return done, len(children)
}

func buildChildLanes(buckets []domain.Bucket, children []domain.Task, finalBucket string) ChildLaneSet {
	ordered := append([]domain.Bucket(nil), buckets...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Position != ordered[j].Position {
			return ordered[i].Position < ordered[j].Position
		}
		return ordered[i].Key < ordered[j].Key
	})
	if len(ordered) == 0 {
		return ChildLaneSet{
			Lanes:     []ChildLane{{Tasks: cloneTasks(children)}},
			Completed: countCompleted(children, finalBucket),
			Total:     len(children),
		}
	}

	lanes := make([]ChildLane, len(ordered))
	for i, bucket := range ordered {
		lanes[i].Bucket = bucket
		for _, child := range children {
			if child.BucketKey == bucket.Key {
				lanes[i].Tasks = append(lanes[i].Tasks, child)
			}
		}
	}
	return ChildLaneSet{
		Lanes:     lanes,
		Completed: countCompleted(children, finalBucket),
		Total:     len(children),
	}
}

func cloneChildLaneSet(set ChildLaneSet) ChildLaneSet {
	lanes := make([]ChildLane, len(set.Lanes))
	for i, lane := range set.Lanes {
		lanes[i] = ChildLane{Bucket: lane.Bucket, Tasks: cloneTasks(lane.Tasks)}
	}
	return ChildLaneSet{Lanes: lanes, Completed: set.Completed, Total: set.Total}
}

func countCompleted(children []domain.Task, finalBucket string) int {
	if finalBucket == "" {
		return 0
	}
	count := 0
	for _, child := range children {
		if child.BucketKey == finalBucket {
			count++
		}
	}
	return count
}

// Buckets returns the workflow buckets in configured order.
func (p Projection) Buckets() []domain.Bucket { return append([]domain.Bucket(nil), p.buckets...) }

// Priority returns a configured priority definition.
func (p Projection) Priority(id domain.Priority) (config.PriorityDefinition, bool) {
	if id == domain.PriorityZero {
		return config.PriorityDefinition{}, false
	}
	for _, definition := range p.priorities {
		if domain.Priority(definition.ID) == id {
			return definition, true
		}
	}
	return config.PriorityDefinition{}, false
}

// PriorityLabel resolves a priority for row/detail metadata.
func (p Projection) PriorityLabel(id domain.Priority) string {
	if definition, ok := p.Priority(id); ok {
		return definition.Value
	}
	return fmt.Sprintf("%d", id)
}

// Fingerprint identifies the snapshot used to build the projection.
func (p Projection) Fingerprint() uint64 { return p.fingerprint }

func (p Projection) allowsPriority(allowed map[string]struct{}, priority domain.Priority) bool {
	if allowed == nil {
		return true
	}
	definition, ok := p.Priority(priority)
	return ok && allows(allowed, definition.Value)
}

func (p Projection) parentChain(taskID int64) []int64 {
	chain := make([]int64, 0, 4)
	seen := map[int64]bool{}
	for task, steps := p.byID[taskID], 0; task.ParentID != nil && steps < len(p.byID); steps++ {
		parentID := *task.ParentID
		if seen[parentID] {
			break
		}
		seen[parentID] = true
		chain = append(chain, parentID)
		parent, ok := p.byID[parentID]
		if !ok {
			break
		}
		task = parent
	}
	return chain
}

func sortTasks(tasks []domain.Task) {
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
}

func sortProjectionTasks(tasks []domain.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].ID != tasks[j].ID {
			return tasks[i].ID < tasks[j].ID
		}
		return strings.ToLower(tasks[i].Title) < strings.ToLower(tasks[j].Title)
	})
}

func cloneTasks(tasks []domain.Task) []domain.Task { return append([]domain.Task(nil), tasks...) }

func lessTask(a, b domain.Task, field string) bool {
	switch field {
	case "title":
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	case "priority":
		return a.Priority < b.Priority
	case "created_at":
		return a.CreatedAt < b.CreatedAt
	default:
		return a.ID < b.ID
	}
}

func stringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func allows(set map[string]struct{}, value string) bool {
	if set == nil {
		return true
	}
	_, ok := set[value]
	return ok
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fingerprint(p Projection) uint64 {
	var hash uint64 = 1469598103934665603
	add := func(value string) {
		for i := 0; i < len(value); i++ {
			hash ^= uint64(value[i])
			hash *= 1099511628211
		}
	}
	for _, task := range p.tasks {
		add(fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%s", task.ID, task.Title, task.BucketKey, task.Priority, task.State))
		if task.ParentID != nil {
			add(fmt.Sprintf("\x00%d", *task.ParentID))
		}
	}
	type blockerPair struct{ taskID, blockerID int64 }
	pairs := make([]blockerPair, 0)
	for taskID, blockers := range p.blockers {
		for _, blocker := range blockers {
			pairs = append(pairs, blockerPair{taskID: taskID, blockerID: blocker.ID})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].taskID != pairs[j].taskID {
			return pairs[i].taskID < pairs[j].taskID
		}
		return pairs[i].blockerID < pairs[j].blockerID
	})
	for _, pair := range pairs {
		add(fmt.Sprintf("%d\x00%d\x00", pair.taskID, pair.blockerID))
	}
	for _, bucket := range p.buckets {
		add(bucket.Key + "\x00" + bucket.Name)
	}
	for _, priority := range p.priorities {
		add(fmt.Sprintf("%d\x00%s\x00%s", priority.ID, priority.Value, priority.Color))
	}
	for _, task := range p.tasks {
		counts := p.badges[task.ID]
		add(fmt.Sprintf("%d\x00%d\x00%d\x00%d", task.ID, counts.Blockers, counts.Comments, counts.Subtasks))
	}
	return hash
}
