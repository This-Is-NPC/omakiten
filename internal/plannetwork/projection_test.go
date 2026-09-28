package plannetwork

import (
	"reflect"
	"testing"

	"omakiten/internal/domain"
)

func TestBuildOrdersRailsAndProjectsSemanticFlags(t *testing.T) {
	parent := int64(10)
	show := domain.PlanShow{
		ActiveWaveID: 20,
		Waves: []domain.PlanWaveView{
			{Wave: domain.PlanWave{ID: 20, Position: 2, Name: "second"}, Tasks: []domain.PlanTaskRow{
				{TaskID: 20, Title: "root", BucketKey: "dev", AssignedTo: "agent"},
				{TaskID: 30, Title: "child", BucketKey: "backlog"},
			}},
			{Wave: domain.PlanWave{ID: 10, Position: 1, Name: "first"}, Tasks: []domain.PlanTaskRow{
				{TaskID: 10, Title: "done", BucketKey: "done"},
			}},
		},
		Dependencies: []domain.TaskDependency{{TaskID: 30, DependsOnTaskID: 20}, {TaskID: 20, DependsOnTaskID: 10}},
	}
	got := Build(Input{
		Show:            show,
		Tasks:           map[int64]domain.Task{30: {ID: 30, ParentID: &parent}},
		FirstBucket:     "backlog",
		FinalBucket:     "done",
		NextClaimableID: 30,
	})

	ids := make([]int64, 0, len(got.Rows))
	for _, row := range got.Rows {
		if row.Kind == TaskCard {
			ids = append(ids, row.Task.TaskID)
		}
	}
	if want := []int64{10, 20, 30}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("task order = %v, want %v", ids, want)
	}
	var inProgress, child Row
	for _, row := range got.Rows {
		if row.Task.TaskID == 20 {
			inProgress = row
		}
		if row.Task.TaskID == 30 {
			child = row
		}
	}
	if inProgress.Status != StatusInProgress || !inProgress.InProgress {
		t.Fatalf("in-progress row = %+v", inProgress)
	}
	if child.ParentID == nil || *child.ParentID != 10 || child.Status != StatusBlocked || !child.IsNext {
		t.Fatalf("child projection = %+v", child)
	}
	if len(got.Filaments) != 1 || got.LaneCount != 1 {
		t.Fatalf("filaments = %+v lanes=%d", got.Filaments, got.LaneCount)
	}
}

func TestBuildStatusPrecedence(t *testing.T) {
	cases := map[string]struct {
		row  Row
		want Status
	}{
		"done":        {Row{FinalBucket: true, Gated: true, InProgress: true, BlockerCount: 1, IsNext: true}, StatusDone},
		"gated":       {Row{Gated: true, InProgress: true, BlockerCount: 1}, StatusGated},
		"in progress": {Row{InProgress: true, BlockerCount: 1}, StatusInProgress},
		"blocked":     {Row{BlockerCount: 1, Task: domain.PlanTaskRow{AssignedTo: "agent"}}, StatusBlocked},
		"assigned":    {Row{Task: domain.PlanTaskRow{AssignedTo: "agent"}, IsNext: true}, StatusAssigned},
		"next":        {Row{IsNext: true}, StatusNext},
		"ready":       {Row{}, StatusReady},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := classifyStatus(tc.row); got != tc.want {
				t.Fatalf("status = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGroupDependenciesIsDeterministic(t *testing.T) {
	got := GroupDependencies([]domain.TaskDependency{
		{TaskID: 4, DependsOnTaskID: 9},
		{TaskID: 3, DependsOnTaskID: 8},
		{TaskID: 4, DependsOnTaskID: 2},
	})
	want := []DependencyGroup{{TaskID: 3, BlockerIDs: []int64{8}}, {TaskID: 4, BlockerIDs: []int64{2, 9}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %+v, want %+v", got, want)
	}
}

func TestBuildRowsOmitsPaintOnlyMetadataAndHonorsCollapse(t *testing.T) {
	show := domain.PlanShow{
		Waves:        []domain.PlanWaveView{{Wave: domain.PlanWave{ID: 1, Position: 1}, Tasks: []domain.PlanTaskRow{{TaskID: 1}}}},
		Dependencies: []domain.TaskDependency{{TaskID: 1, DependsOnTaskID: 2}},
	}
	got := BuildRows(Input{Show: show, Collapsed: map[int64]bool{1: true}})
	if len(got.Rows) != 1 || got.Rows[0].Kind != WaveHeader {
		t.Fatalf("collapsed rows = %+v", got.Rows)
	}
	if got.Filaments != nil || got.Rows[0].IsCritical {
		t.Fatalf("row-only build contains paint metadata: %+v", got)
	}
}
