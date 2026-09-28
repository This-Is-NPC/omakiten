package taskprojection

import (
	"reflect"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestBuildProjectsRelationsFiltersAndCompletion(t *testing.T) {
	parent, root := int64(2), int64(1)
	p := Build(Input{
		Tasks: []domain.Task{
			{ID: 3, Title: "child done", BucketKey: "done", ParentID: &parent, Priority: 2},
			{ID: 2, Title: "parent", BucketKey: "dev", ParentID: &root, Priority: 1},
			{ID: 1, Title: "root", BucketKey: "backlog", Priority: 2},
			{ID: 4, Title: "other", BucketKey: "review", Priority: 3},
		},
		Workflow:     domain.Workflow{Buckets: []domain.Bucket{{Key: "backlog", Position: 1}, {Key: "dev", Position: 2}, {Key: "done", Position: 3}}},
		Dependencies: []domain.TaskDependency{{TaskID: 1, DependsOnTaskID: 4}, {TaskID: 1, DependsOnTaskID: 3}},
		Comments:     []domain.Comment{{TaskID: 1}, {TaskID: 1}},
		Priorities:   []config.PriorityDefinition{{ID: 1, Value: "low"}, {ID: 2, Value: "normal"}, {ID: 3, Value: "high"}},
	})

	if got := ids(p.Tasks(Query{RootOnly: true, Priority: []string{"normal"}})); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("filtered roots = %v", got)
	}
	if got := ids(p.Tasks(Query{Bucket: []string{"review"}, SortField: "id", SortOrder: "desc"})); !reflect.DeepEqual(got, []int64{4}) {
		t.Fatalf("filtered bucket = %v", got)
	}
	if got := ids(p.Children(1)); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("children = %v", got)
	}
	if got := ids(p.Blockers(1)); !reflect.DeepEqual(got, []int64{3, 4}) {
		t.Fatalf("blockers = %v", got)
	}
	if got := p.Ancestors(3); !reflect.DeepEqual(got, []int64{2, 1}) {
		t.Fatalf("ancestors = %v", got)
	}
	if got := p.Badges(1); got != (BadgeCounts{Blockers: 2, Comments: 2, Subtasks: 1}) {
		t.Fatalf("badges = %+v", got)
	}
	if !p.IsComplete(3) || p.IsComplete(2) {
		t.Fatal("final-bucket classification is incorrect")
	}
	if done, total := p.Completion(2); done != 1 || total != 1 {
		t.Fatalf("completion = %d/%d", done, total)
	}
}

func TestChildLanesOrderCountsAndCompletion(t *testing.T) {
	parent := int64(10)
	p := Build(Input{
		Tasks: []domain.Task{
			{ID: 14, Title: "done two", BucketKey: "done", ParentID: &parent},
			{ID: 12, Title: "dev", BucketKey: "dev", ParentID: &parent},
			{ID: 11, Title: "done one", BucketKey: "done", ParentID: &parent},
			{ID: 13, Title: "backlog", BucketKey: "backlog", ParentID: &parent},
		},
		Workflow: domain.Workflow{Buckets: []domain.Bucket{
			{Key: "done", Name: "Done", Position: 3},
			{Key: "backlog", Name: "Backlog", Position: 1},
			{Key: "dev", Name: "Development", Position: 2},
		}},
	})

	lanes := p.ChildLanes(parent)
	if got := laneKeys(lanes.Lanes); !reflect.DeepEqual(got, []string{"backlog", "dev", "done"}) {
		t.Fatalf("lane order = %v", got)
	}
	if got := ids(lanes.Lanes[0].Tasks); !reflect.DeepEqual(got, []int64{13}) {
		t.Fatalf("backlog tasks = %v", got)
	}
	if got := ids(lanes.Lanes[2].Tasks); !reflect.DeepEqual(got, []int64{11, 14}) {
		t.Fatalf("done tasks = %v", got)
	}
	if lanes.Completed != 2 || lanes.Total != 4 {
		t.Fatalf("completion = %d/%d", lanes.Completed, lanes.Total)
	}
}

func TestChildLanesFallbackKeepsChildrenAndCounts(t *testing.T) {
	parent := int64(10)
	p := Build(Input{Tasks: []domain.Task{
		{ID: 12, Title: "second", ParentID: &parent},
		{ID: 11, Title: "first", ParentID: &parent},
	}})

	lanes := p.ChildLanes(parent)
	if len(lanes.Lanes) != 1 || lanes.Lanes[0].Bucket.Key != "" {
		t.Fatalf("fallback lanes = %+v", lanes.Lanes)
	}
	if got := ids(lanes.Lanes[0].Tasks); !reflect.DeepEqual(got, []int64{11, 12}) {
		t.Fatalf("fallback tasks = %v", got)
	}
	if lanes.Completed != 0 || lanes.Total != 2 {
		t.Fatalf("fallback completion = %d/%d", lanes.Completed, lanes.Total)
	}
}

func TestTasksUseDeterministicTieBreakers(t *testing.T) {
	p := Build(Input{Tasks: []domain.Task{{ID: 2, Title: "same", CreatedAt: "x"}, {ID: 1, Title: "same", CreatedAt: "x"}, {ID: 3, Title: "same", CreatedAt: "x"}}})
	if got := ids(p.Tasks(Query{SortField: "title"})); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("title ties = %v", got)
	}
	if got := ids(p.Tasks(Query{SortField: "created_at", SortOrder: "desc"})); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("created ties = %v", got)
	}
}

func TestEmptyProjectionHasNoCandidates(t *testing.T) {
	if got := Build(Input{}).Candidates(1); len(got) != 0 {
		t.Fatalf("empty candidates = %v", got)
	}
}

func ids(tasks []domain.Task) []int64 {
	got := make([]int64, len(tasks))
	for i, task := range tasks {
		got[i] = task.ID
	}
	return got
}

func laneKeys(lanes []ChildLane) []string {
	got := make([]string, len(lanes))
	for i, lane := range lanes {
		got[i] = lane.Bucket.Key
	}
	return got
}
