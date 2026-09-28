package plannetwork

import (
	"sort"

	"omakiten/internal/domain"
	graphplan "omakiten/internal/graph/plan"
)

// Input is the in-memory plan snapshot and the workflow facts needed to
// classify its tasks. Collapsed and Tasks are read-only caller state; Build
// does not retain either map.
type Input struct {
	Show            domain.PlanShow
	NextClaimableID int64
	FirstBucket     string
	FinalBucket     string
	Tasks           map[int64]domain.Task
	Collapsed       map[int64]bool
}

// RowKind identifies the two semantic row shapes in the linear outline.
type RowKind uint8

const (
	WaveHeader RowKind = iota
	TaskCard
)

// Status is the product status precedence resolved for a task row. The screen
// chooses localized text and paint from this value.
type Status uint8

const (
	StatusReady Status = iota
	StatusNext
	StatusAssigned
	StatusBlocked
	StatusInProgress
	StatusGated
	StatusDone
)

// Row is one semantic line in the visible outline. Rails and blocker IDs are
// already projected so renderers never traverse dependencies.
type Row struct {
	Kind    RowKind
	WaveID  int64
	WaveIdx int

	WaveName   string
	WavePos    int
	WaveDone   int
	WaveTotal  int
	WaveActive bool
	Collapsed  bool

	Task          domain.PlanTaskRow
	ParentID      *int64
	Rail          string
	IsNext        bool
	IsCritical    bool
	BlockerCount  int
	IntraBlockers []int64
	CrossBlockers []int64
	FinalBucket   bool
	InProgress    bool
	Gated         bool
	Status        Status
}

// DependencyGroup is one deterministic dependent-to-blocker footer group.
type DependencyGroup struct {
	TaskID     int64
	BlockerIDs []int64
}

// Filament describes one cross-wave source fan-out in visible row indexes.
// It mirrors graph/plan's lane result without exposing graph algorithms to the
// screen package.
type Filament struct {
	SrcRow  int
	DstRows []int
	Lane    int
}

// EndRow returns the last destination row reached by the filament.
func (f Filament) EndRow() int {
	if len(f.DstRows) == 0 {
		return f.SrcRow
	}
	return f.DstRows[len(f.DstRows)-1]
}

// Projection is the complete semantic Plan Network projection.
type Projection struct {
	Rows            []Row
	CrossBlockers   map[int64][]int64
	NextClaimableID int64
	Dependencies    []DependencyGroup
	Filaments       []Filament
	LaneCount       int
}

// Build creates the complete semantic projection, including critical-path and
// filament data.
func Build(input Input) Projection { return build(input, true) }

// BuildRows creates the row projection without the critical-path and filament
// work used only by rendering. It is for interaction paths that need row shape
// and cursor targets but do not need paint metadata.
func BuildRows(input Input) Projection { return build(input, false) }

func build(input Input, includePaintMetadata bool) Projection {
	if len(input.Show.Waves) == 0 {
		return Projection{NextClaimableID: input.NextClaimableID, Dependencies: GroupDependencies(input.Show.Dependencies)}
	}

	waves := orderedWaves(input.Show.Waves)
	intraBlockers := graphplan.IntraWaveBlockers(input.Show.Dependencies, waves)
	crossBlockers := graphplan.CrossWaveBlockers(input.Show.Dependencies, waves)
	criticalPath := criticalPath(input.Show.Dependencies, waves, includePaintMetadata)
	rows := projectRows(input, waves, intraBlockers, crossBlockers, criticalPath, includePaintMetadata)
	result := Projection{
		Rows:            rows,
		CrossBlockers:   cloneBlockers(crossBlockers),
		NextClaimableID: input.NextClaimableID,
		Dependencies:    GroupDependencies(input.Show.Dependencies),
	}
	if includePaintMetadata {
		result.Filaments, result.LaneCount = buildFilaments(rows, crossBlockers)
	}
	return result
}

func orderedWaves(waves []domain.PlanWaveView) []domain.PlanWaveView {
	ordered := append([]domain.PlanWaveView(nil), waves...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Wave.Position != ordered[j].Wave.Position {
			return ordered[i].Wave.Position < ordered[j].Wave.Position
		}
		return ordered[i].Wave.ID < ordered[j].Wave.ID
	})
	return ordered
}

func criticalPath(deps []domain.TaskDependency, waves []domain.PlanWaveView, include bool) map[int64]bool {
	if !include {
		return nil
	}
	allTasks := make([]domain.PlanTaskRow, 0)
	for _, wave := range waves {
		allTasks = append(allTasks, wave.Tasks...)
	}
	return graphplan.CriticalPath(deps, allTasks)
}

func projectRows(input Input, waves []domain.PlanWaveView, intraBlockers, crossBlockers map[int64][]int64, criticalPath map[int64]bool, includePaintMetadata bool) []Row {
	rows := make([]Row, 0)
	for waveIdx, wave := range waves {
		collapsed := input.Collapsed[wave.Wave.ID]
		rows = append(rows, Row{
			Kind:       WaveHeader,
			WaveID:     wave.Wave.ID,
			WaveIdx:    waveIdx,
			WaveName:   wave.Wave.Name,
			WavePos:    wave.Wave.Position,
			WaveDone:   wave.DoneCount,
			WaveTotal:  wave.TotalCount,
			WaveActive: wave.Wave.ID == input.Show.ActiveWaveID,
			Collapsed:  collapsed,
		})
		if collapsed {
			continue
		}

		rows = append(rows, projectWaveTasks(input, wave, waveIdx, intraBlockers, crossBlockers, criticalPath, includePaintMetadata)...)
	}
	return rows
}

func projectWaveTasks(input Input, wave domain.PlanWaveView, waveIdx int, intraBlockers, crossBlockers map[int64][]int64, criticalPath map[int64]bool, includePaintMetadata bool) []Row {
	layout := graphplan.BuildWaveRails(wave.Tasks, intraBlockers)
	rows := make([]Row, 0, len(layout.OrderedIdx))
	for pos, taskIdx := range layout.OrderedIdx {
		task := wave.Tasks[taskIdx]
		var parentID *int64
		if includePaintMetadata {
			parentID = parentIDFor(input.Tasks, task.TaskID)
		}
		intra := intraBlockers[task.TaskID]
		cross := crossBlockers[task.TaskID]
		row := Row{
			Kind:          TaskCard,
			WaveID:        wave.Wave.ID,
			WaveIdx:       waveIdx,
			Task:          task,
			ParentID:      parentID,
			Rail:          layout.Rails[pos],
			IsNext:        task.TaskID == input.NextClaimableID,
			IsCritical:    criticalPath[task.TaskID],
			BlockerCount:  len(intra) + len(cross),
			IntraBlockers: graphplan.ExcludeID(intra, layout.ParentByPos[pos]),
			CrossBlockers: append([]int64(nil), cross...),
			FinalBucket:   input.FinalBucket != "" && task.BucketKey == input.FinalBucket,
			InProgress:    input.FinalBucket != "" && input.FirstBucket != "" && task.BucketKey != input.FinalBucket && task.BucketKey != input.FirstBucket,
			Gated:         input.Show.ActiveWaveID != 0 && wave.Wave.ID != input.Show.ActiveWaveID,
		}
		row.Status = classifyStatus(row)
		rows = append(rows, row)
	}
	return rows
}

// GroupDependencies groups blockers by dependent task and sorts both levels.
func GroupDependencies(deps []domain.TaskDependency) []DependencyGroup {
	byTask := make(map[int64][]int64)
	for _, dep := range deps {
		byTask[dep.TaskID] = append(byTask[dep.TaskID], dep.DependsOnTaskID)
	}
	keys := make([]int64, 0, len(byTask))
	for taskID := range byTask {
		keys = append(keys, taskID)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	groups := make([]DependencyGroup, 0, len(keys))
	for _, taskID := range keys {
		blockers := append([]int64(nil), byTask[taskID]...)
		sort.Slice(blockers, func(i, j int) bool { return blockers[i] < blockers[j] })
		groups = append(groups, DependencyGroup{TaskID: taskID, BlockerIDs: blockers})
	}
	return groups
}

// CompletionPercent delegates plan completion math to the shared graph
// package so callers do not need to import graph algorithms for a header value.
func CompletionPercent(done, total int) int { return graphplan.Percent(done, total) }

// FilamentSourceIDsAtRow returns source IDs whose cross-wave edge is already
// represented by a filament terminating on the requested row.
func FilamentSourceIDsAtRow(filaments []Filament, rows []Row, rowIdx int) map[int64]bool {
	if rowIdx < 0 || rowIdx >= len(rows) {
		return nil
	}
	result := map[int64]bool{}
	for _, filament := range filaments {
		for _, destination := range filament.DstRows {
			if destination != rowIdx {
				continue
			}
			source := rows[filament.SrcRow]
			if source.Kind == TaskCard {
				result[source.Task.TaskID] = true
			}
			break
		}
	}
	return result
}

func classifyStatus(row Row) Status {
	switch {
	case row.FinalBucket:
		return StatusDone
	case row.Gated:
		return StatusGated
	case row.InProgress:
		return StatusInProgress
	case row.BlockerCount > 0:
		return StatusBlocked
	case row.Task.AssignedTo != "":
		return StatusAssigned
	case row.IsNext:
		return StatusNext
	default:
		return StatusReady
	}
}

func parentIDFor(tasks map[int64]domain.Task, taskID int64) *int64 {
	task, ok := tasks[taskID]
	if !ok || task.ParentID == nil {
		return nil
	}
	id := *task.ParentID
	return &id
}

func cloneBlockers(blockers map[int64][]int64) map[int64][]int64 {
	if len(blockers) == 0 {
		return nil
	}
	result := make(map[int64][]int64, len(blockers))
	for taskID, ids := range blockers {
		result[taskID] = append([]int64(nil), ids...)
	}
	return result
}

func buildFilaments(rows []Row, blockers map[int64][]int64) ([]Filament, int) {
	if len(rows) == 0 || len(blockers) == 0 {
		return nil, 0
	}
	rowByTask := make(map[int64]int, len(rows))
	for index, row := range rows {
		if row.Kind == TaskCard {
			rowByTask[row.Task.TaskID] = index
		}
	}
	graphFilaments, laneCount := graphplan.Lanes(rowByTask, blockers)
	filaments := make([]Filament, len(graphFilaments))
	for index, filament := range graphFilaments {
		filaments[index] = Filament{SrcRow: filament.SrcRow, DstRows: append([]int(nil), filament.DstRows...), Lane: filament.Lane}
	}
	return filaments, laneCount
}
