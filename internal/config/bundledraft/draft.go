package bundledraft

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"omakiten/internal/config"
)

type Draft struct {
	editor         Editor
	text           Text
	original       config.Bundle
	candidate      config.Bundle
	sourcePath     string
	baselineHashes map[string]string
	plan           config.Bundle
	dirty          bool
	blocked        string
}

type Report struct {
	Original        config.Bundle
	Candidate       config.Bundle
	Dirty           bool
	ValidationError error
	EditWarnings    []string
	DiffSummary     []string
	BlockedReason   string
}

type ImpactPreview struct {
	RemovedBuckets     []BucketImpact
	BucketKeyChanges   []BucketKeyChange
	FinalBucketChanged *FinalBucketChange
	TaskCountError     error
}

type BucketImpact struct {
	ID              int
	Key             string
	Name            string
	ActiveTaskCount int
}

type BucketKeyChange struct {
	ID              int
	FromKey         string
	ToKey           string
	ActiveTaskCount int
}

type FinalBucketChange struct {
	FromKey string
	ToKey   string
}

type BucketPermissionEntity string

const (
	BucketPermissionTask    BucketPermissionEntity = "task"
	BucketPermissionComment BucketPermissionEntity = "comment"
)

type BucketPermissionOp string

const (
	BucketPermissionCreate BucketPermissionOp = "create"
	BucketPermissionEdit   BucketPermissionOp = "edit"
	BucketPermissionDelete BucketPermissionOp = "delete"
)

type GuardSetKind string

const (
	GuardSetTransition GuardSetKind = "transition"
	GuardSetArchive    GuardSetKind = "archive"
	GuardSetDelete     GuardSetKind = "delete"
	GuardSetUnarchive  GuardSetKind = "unarchive"
)

type BucketTaskCounter interface {
	CountActiveTasksByBucket(ctx context.Context, bucketKeys []string) (map[string]int, error)
}

func New(editor Editor) (*Draft, error) {
	original, _, baselineHashes, err := editor.LoadPlan()
	if err != nil {
		return nil, err
	}
	d := &Draft{
		editor:         editor,
		original:       CloneBundle(original),
		candidate:      CloneBundle(original),
		plan:           CloneBundle(original),
		sourcePath:     editor.Path(),
		baselineHashes: cloneHashes(baselineHashes)}
	d.blocked = importedBlockReason(original, nil)
	return d, nil
}

// ApplyPlanned carries the caller's loaded bundle and wiring version through
// the editor apply boundary without establishing a second baseline.
func ApplyPlanned(ctx context.Context, editor Editor, mutate func(*config.Bundle) error) (config.Bundle, error) {
	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		return config.Bundle{}, err
	}
	return editor.Apply(ctx, bundle, sourceHashes, mutate)
}

func (d *Draft) AssuranceSnapshot() (string, error) {
	if err := d.ensureCurrentBaseline(); err != nil {
		return "", err
	}
	return d.sourcePath + "\x00" + d.baselineHashes[d.sourcePath], nil
}

func (d *Draft) BindText(text Text) {
	if d != nil {
		d.text = text
	}
}

func (d *Draft) errf(key, fallback string, args ...any) error {
	return fmt.Errorf("%s", Tr(d.text, key, fallback, args...))
}

func (d *Draft) wrapf(err error, key, fallback string, args ...any) error {
	return fmt.Errorf("%s: %w", Tr(d.text, key, fallback, args...), err)
}

func (d *Draft) ensureCurrentBaseline() error {
	if d.editor.Path() != d.sourcePath {
		return d.errf("tui.studio.err.source_changed", "studio draft source changed from %q to %q; reopen Studio before applying", d.sourcePath, d.editor.Path())
	}
	current, _, hashes, err := d.editor.LoadPlan()
	if err != nil {
		return d.wrapf(err, "tui.studio.err.check_baseline", "check Studio baseline")
	}
	if hashes[d.sourcePath] != d.baselineHashes[d.sourcePath] {
		return d.errf("tui.studio.err.baseline_changed", "studio draft baseline changed on disk; reload Studio before applying")
	}
	if !reflect.DeepEqual(current, d.original) {
		return d.errf("tui.studio.err.baseline_changed", "studio draft baseline changed on disk; reload Studio before applying")
	}
	return nil
}

func (d *Draft) Report() Report {
	return d.ReportText(d.text)
}

// Candidate is the draft's working bundle for the paint paths that only
// READ it.
//
// It is not a clone, and that is the whole point. [Draft.ReportText] deep
// clones BOTH bundles, revalidates the candidate, recomputes the edit warnings
// and diffs the pair — and a Studio paint asked for it once per body, with a
// keystroke rendering every body two or three times. Scrolling a persona's
// RELATED list spent nearly half of every keystroke cloning a bundle nobody had
// edited, which is what "o desempenho fica um lixo" was.
//
// The contract is READ ONLY, and it is a contract the callers already keep: a
// renderer wants the values, not a copy it may scribble on, and every mutation
// goes through [Draft.Mutate], which clones before it writes. The report
// stays for the three places that genuinely need the diff, the validation error
// and the dirty flag.
func (d *Draft) Candidate() config.Bundle {
	if d == nil {
		return config.Bundle{}
	}
	return d.candidate
}

// Dirty reports whether the candidate has diverged from the baseline the draft
// opened on. It is the cheap read the paint paths want; [Draft.Report] carries
// the same flag but pays for two deep clones and a revalidation to say it.
func (d *Draft) Dirty() bool {
	return d != nil && d.dirty
}

// ReportText is Report with an i18n resolver for DiffSummary and related
// operator-facing copy. Paint paths pass Screen.t so the preview matches the
// active catalog; domain tests leave text nil and assert the English fallback.
func (d *Draft) ReportText(text Text) Report {
	if text == nil {
		text = d.text
	}
	validationErr := d.validateCandidate()
	return Report{
		Original:        CloneBundle(d.original),
		Candidate:       CloneBundle(d.candidate),
		Dirty:           d.dirty,
		ValidationError: validationErr,
		EditWarnings:    editWarnings(d.candidate, text),
		DiffSummary:     Diff(d.original, d.candidate, text),
		BlockedReason:   importedBlockReason(d.original, text)}
}

func (d *Draft) Mutate(mutate func(*config.Bundle) error) Report {
	next := CloneBundle(d.candidate)
	if err := mutate(&next); err != nil {
		report := d.Report()
		report.ValidationError = err
		return report
	}
	d.candidate = next
	d.dirty = !reflect.DeepEqual(d.original, d.candidate)
	return d.Report()
}

func (d *Draft) SetCommandSpec(key string, spec config.CommandSpec) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		key = strings.TrimSpace(key)
		if key == "" {
			return d.errf("tui.studio.err.command_key_required", "command key is required")
		}
		if bundle.Commands == nil {
			bundle.Commands = map[string]config.CommandSpec{}
		}
		bundle.Commands[key] = config.CommandSpec{
			Persona:      strings.TrimSpace(spec.Persona),
			Laws:         append([]string(nil), spec.Laws...),
			LawsDisabled: append([]string(nil), spec.LawsDisabled...),
			Templates:    append([]string(nil), spec.Templates...),
			Skills:       append([]string(nil), spec.Skills...)}
		return nil
	})
}

func (d *Draft) RenameBucket(bucketID int, name string) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		bucket := activeWorkflowBucket(bundle, bucketID)
		if bucket == nil {
			return d.errf("tui.studio.err.bucket_id_not_found", "bucket id %d not found", bucketID)
		}
		bucket.Name = strings.TrimSpace(name)
		return nil
	})
}

func (d *Draft) ChangeBucketKey(bucketID int, key string) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		workflow := activeWorkflowPtr(bundle)
		if workflow == nil {
			return d.errf("tui.studio.err.active_workflow_not_found", "active workflow not found")
		}
		bucket := workflowBucket(workflow, bucketID)
		if bucket == nil {
			return d.errf("tui.studio.err.bucket_id_not_found", "bucket id %d not found", bucketID)
		}
		oldKey := bucket.Key
		newKey := strings.TrimSpace(key)
		bucket.Key = newKey
		if oldKey != newKey {
			updateBucketKeyReferences(bundle, workflow, oldKey, newKey)
		}
		return nil
	})
}

func (d *Draft) AddBucket(key, name string) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		workflow := activeWorkflowPtr(bundle)
		if workflow == nil {
			return d.errf("tui.studio.err.active_workflow_not_found", "active workflow not found")
		}
		maxID, maxPosition := 0, 0
		for _, bucket := range workflow.Buckets {
			if bucket.ID > maxID {
				maxID = bucket.ID
			}
			if bucket.Position > maxPosition {
				maxPosition = bucket.Position
			}
		}
		workflow.Buckets = append(workflow.Buckets, config.Bucket{
			ID:       maxID + 1,
			Key:      strings.TrimSpace(key),
			Name:     strings.TrimSpace(name),
			Position: maxPosition + 1})
		return nil
	})
}

func (d *Draft) MoveBucket(bucketID int, delta int) Report {
	return d.Mutate(func(bundle *config.Bundle) error { return d.moveBucket(bundle, bucketID, delta) })
}

func (d *Draft) moveBucket(bundle *config.Bundle, bucketID, delta int) error {
	workflow := activeWorkflowPtr(bundle)
	if workflow == nil {
		return d.errf("tui.studio.err.active_workflow_not_found", "active workflow not found")
	}
	ordered := OrderedBuckets(workflow.Buckets)
	idx := bucketIndex(ordered, bucketID)
	if idx < 0 {
		return d.errf("tui.studio.err.bucket_id_not_found", "bucket id %d not found", bucketID)
	}
	target := idx + delta
	if target < 0 || target >= len(ordered) {
		return d.errf("tui.studio.err.bucket_cannot_move", "bucket %s cannot move further", ordered[idx].Key)
	}
	ordered[idx], ordered[target] = ordered[target], ordered[idx]
	for i := range ordered {
		ordered[i].Position = i + 1
	}
	positions := map[int]int{}
	for _, bucket := range ordered {
		positions[bucket.ID] = bucket.Position
	}
	for i := range workflow.Buckets {
		workflow.Buckets[i].Position = positions[workflow.Buckets[i].ID]
	}
	return nil
}

func bucketIndex(buckets []config.Bucket, bucketID int) int {
	for i, bucket := range buckets {
		if bucket.ID == bucketID {
			return i
		}
	}
	return -1
}

func (d *Draft) SetBucketPermission(bucketID int, entity BucketPermissionEntity, op BucketPermissionOp, allowed bool) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		return d.setBucketPermission(bundle, bucketID, entity, op, allowed)
	})
}

func (d *Draft) setBucketPermission(bundle *config.Bundle, bucketID int, entity BucketPermissionEntity, op BucketPermissionOp, allowed bool) error {
	bucket := activeWorkflowBucket(bundle, bucketID)
	if bucket == nil {
		return d.errf("tui.studio.err.bucket_id_not_found", "bucket id %d not found", bucketID)
	}
	if entity == BucketPermissionTask && op == BucketPermissionCreate {
		return d.errf("tui.studio.err.task_create_permission_unsupported", "task create permission is not supported")
	}
	if entity != BucketPermissionTask && entity != BucketPermissionComment {
		return d.errf("tui.studio.err.unknown_permission_entity", "unknown bucket permission entity %q", entity)
	}
	if op != BucketPermissionCreate && op != BucketPermissionEdit && op != BucketPermissionDelete {
		return d.errf("tui.studio.err.unknown_permission_operation", "unknown bucket permission operation %q", op)
	}
	if bucket.Permissions == nil {
		bucket.Permissions = &config.BucketPermissions{}
	}
	permission := bucket.Permissions.Task
	if entity == BucketPermissionComment {
		permission = bucket.Permissions.Comment
	}
	if permission == nil {
		permission = &config.EntityPermission{}
		if entity == BucketPermissionTask {
			bucket.Permissions.Task = permission
		} else {
			bucket.Permissions.Comment = permission
		}
	}
	setEntityPermissionBool(permission, op, allowed)
	return nil
}

func (d *Draft) AddTransition(fromBucketID, toBucketID int) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		workflow := activeWorkflowPtr(bundle)
		if workflow == nil {
			return d.errf("tui.studio.err.active_workflow_not_found", "active workflow not found")
		}
		if fromBucketID == toBucketID {
			return d.errf("tui.studio.err.self_transition_unsupported", "self-transitions are not supported")
		}
		if workflowBucket(workflow, fromBucketID) == nil {
			return d.errf("tui.studio.err.from_bucket_id_not_found", "from bucket id %d not found", fromBucketID)
		}
		if workflowBucket(workflow, toBucketID) == nil {
			return d.errf("tui.studio.err.to_bucket_id_not_found", "to bucket id %d not found", toBucketID)
		}
		for _, transition := range workflow.Transitions {
			if transition.From == fromBucketID && transition.To == toBucketID {
				return d.errf("tui.studio.err.transition_exists", "transition already exists")
			}
		}
		workflow.Transitions = append(workflow.Transitions, config.Transition{From: fromBucketID, To: toBucketID})
		return nil
	})
}

func (d *Draft) RemoveTransition(fromBucketID, toBucketID int) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		workflow := activeWorkflowPtr(bundle)
		if err := d.validateTransition(workflow, fromBucketID, toBucketID); err != nil {
			return err
		}
		removed := false
		out := workflow.Transitions[:0]
		for _, transition := range workflow.Transitions {
			if transition.From == fromBucketID && transition.To == toBucketID {
				removed = true
				continue
			}
			out = append(out, transition)
		}
		if !removed {
			return d.errf("tui.studio.err.transition_missing", "transition does not exist")
		}
		workflow.Transitions = out
		return nil
	})
}

func (d *Draft) validateTransition(workflow *config.Workflow, fromBucketID, toBucketID int) error {
	if workflow == nil {
		return d.errf("tui.studio.err.active_workflow_not_found", "active workflow not found")
	}
	if fromBucketID == toBucketID {
		return d.errf("tui.studio.err.self_transition_unsupported", "self-transitions are not supported")
	}
	if workflowBucket(workflow, fromBucketID) == nil {
		return d.errf("tui.studio.err.from_bucket_id_not_found", "from bucket id %d not found", fromBucketID)
	}
	if workflowBucket(workflow, toBucketID) == nil {
		return d.errf("tui.studio.err.to_bucket_id_not_found", "to bucket id %d not found", toBucketID)
	}
	return nil
}

func (d *Draft) AddGuard(kind GuardSetKind, fromBucketID, toBucketID int, guard config.TransitionGuard) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		guards, err := d.guardSlice(bundle, kind, fromBucketID, toBucketID)
		if err != nil {
			return err
		}
		*guards = append(*guards, guard)
		return nil
	})
}

func (d *Draft) SetGuard(kind GuardSetKind, fromBucketID, toBucketID int, index int, guard config.TransitionGuard) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		guards, err := d.guardSlice(bundle, kind, fromBucketID, toBucketID)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(*guards) {
			return d.errf("tui.studio.err.guard_index_not_found", "guard index %d not found", index)
		}
		(*guards)[index] = guard
		return nil
	})
}

func (d *Draft) RemoveGuard(kind GuardSetKind, fromBucketID, toBucketID int, index int) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		guards, err := d.guardSlice(bundle, kind, fromBucketID, toBucketID)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(*guards) {
			return d.errf("tui.studio.err.guard_index_not_found", "guard index %d not found", index)
		}
		*guards = append((*guards)[:index], (*guards)[index+1:]...)
		return nil
	})
}

func (d *Draft) MoveGuard(kind GuardSetKind, fromBucketID, toBucketID int, index, delta int) Report {
	return d.Mutate(func(bundle *config.Bundle) error {
		guards, err := d.guardSlice(bundle, kind, fromBucketID, toBucketID)
		if err != nil {
			return err
		}
		target := index + delta
		if index < 0 || index >= len(*guards) || target < 0 || target >= len(*guards) {
			return d.errf("tui.studio.err.guard_cannot_move", "guard cannot move further")
		}
		(*guards)[index], (*guards)[target] = (*guards)[target], (*guards)[index]
		return nil
	})
}

func (d *Draft) guardSlice(bundle *config.Bundle, kind GuardSetKind, fromBucketID, toBucketID int) (*[]config.TransitionGuard, error) {
	workflow := activeWorkflowPtr(bundle)
	if workflow == nil {
		return nil, d.errf("tui.studio.err.active_workflow_not_found", "active workflow not found")
	}
	switch kind {
	case GuardSetTransition:
		for i := range workflow.Transitions {
			if workflow.Transitions[i].From == fromBucketID && workflow.Transitions[i].To == toBucketID {
				return &workflow.Transitions[i].Guards, nil
			}
		}
		return nil, d.errf("tui.studio.err.transition_missing", "transition does not exist")
	case GuardSetArchive:
		return &workflow.Operations.Archive.Guards, nil
	case GuardSetDelete:
		return &workflow.Operations.Delete.Guards, nil
	case GuardSetUnarchive:
		return &workflow.Operations.Unarchive.Guards, nil
	default:
		return nil, d.errf("tui.studio.err.unknown_guard_set", "unknown guard set %q", kind)
	}
}

func (d *Draft) DeleteBucket(ctx context.Context, counter BucketTaskCounter, bucketID int) Report {
	var deletedKey string
	report := d.Mutate(func(bundle *config.Bundle) error {
		workflow := activeWorkflowPtr(bundle)
		if workflow == nil {
			return d.errf("tui.studio.err.active_workflow_not_found", "active workflow not found")
		}
		idx := bucketIndex(workflow.Buckets, bucketID)
		if idx < 0 {
			return d.errf("tui.studio.err.bucket_id_not_found", "bucket id %d not found", bucketID)
		}
		deletedKey = workflow.Buckets[idx].Key
		if err := d.checkBucketDelete(ctx, counter, deletedKey); err != nil {
			return err
		}
		workflow.Buckets = append(workflow.Buckets[:idx], workflow.Buckets[idx+1:]...)
		workflow.Transitions = removeBucketTransitions(workflow.Transitions, bucketID)
		if err := removeBucketGuardReferences(workflow, deletedKey, d.text); err != nil {
			return err
		}
		normalizeBucketPositions(workflow.Buckets)
		return nil
	})
	return report
}

func (d *Draft) checkBucketDelete(ctx context.Context, counter BucketTaskCounter, key string) error {
	if counter == nil || key == "" {
		return nil
	}
	counts, err := counter.CountActiveTasksByBucket(ctx, []string{key})
	if err != nil {
		return err
	}
	if counts[key] > 0 {
		return d.errf("tui.studio.err.bucket_delete_active_tasks", "bucket %q has %d active task(s); delete is blocked", key, counts[key])
	}
	return nil
}

func (d *Draft) ImpactPreview(ctx context.Context, counter BucketTaskCounter) ImpactPreview {
	original, okOriginal := ActiveWorkflow(d.original)
	candidate, okCandidate := ActiveWorkflow(d.candidate)
	if !okOriginal || !okCandidate {
		return ImpactPreview{}
	}
	preview := impactPreview(original, candidate)
	return d.addImpactTaskCounts(ctx, counter, preview)
}

func impactPreview(original, candidate config.Workflow) ImpactPreview {
	preview := ImpactPreview{}
	candidateByID := bucketsByID(candidate.Buckets)
	for _, bucket := range original.Buckets {
		if _, ok := candidateByID[bucket.ID]; !ok {
			preview.RemovedBuckets = append(preview.RemovedBuckets, BucketImpact{ID: bucket.ID, Key: bucket.Key, Name: bucket.Name})
		}
	}

	originalByID := bucketsByID(original.Buckets)
	for _, bucket := range candidate.Buckets {
		before, ok := originalByID[bucket.ID]
		if ok && before.Key != bucket.Key {
			preview.BucketKeyChanges = append(preview.BucketKeyChanges, BucketKeyChange{ID: bucket.ID, FromKey: before.Key, ToKey: bucket.Key})
		}
	}
	sort.Slice(preview.RemovedBuckets, func(i, j int) bool { return preview.RemovedBuckets[i].ID < preview.RemovedBuckets[j].ID })
	sort.Slice(preview.BucketKeyChanges, func(i, j int) bool { return preview.BucketKeyChanges[i].ID < preview.BucketKeyChanges[j].ID })

	originalFinal := finalBucketKey(original.Buckets)
	candidateFinal := finalBucketKey(candidate.Buckets)
	if originalFinal != candidateFinal {
		preview.FinalBucketChanged = &FinalBucketChange{FromKey: originalFinal, ToKey: candidateFinal}
	}
	return preview
}

func (d *Draft) addImpactTaskCounts(ctx context.Context, counter BucketTaskCounter, preview ImpactPreview) ImpactPreview {
	affectedKeys := map[string]struct{}{}
	for _, bucket := range preview.RemovedBuckets {
		affectedKeys[bucket.Key] = struct{}{}
	}
	for _, change := range preview.BucketKeyChanges {
		affectedKeys[change.FromKey] = struct{}{}
	}
	if preview.FinalBucketChanged != nil {
		affectedKeys[preview.FinalBucketChanged.FromKey] = struct{}{}
	}
	if counter == nil || len(affectedKeys) == 0 {
		return preview
	}
	keys := make([]string, 0, len(affectedKeys))
	for key := range affectedKeys {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	counts, err := counter.CountActiveTasksByBucket(ctx, keys)
	if err != nil {
		preview.TaskCountError = err
		return preview
	}
	for i := range preview.RemovedBuckets {
		preview.RemovedBuckets[i].ActiveTaskCount = counts[preview.RemovedBuckets[i].Key]
	}
	for i := range preview.BucketKeyChanges {
		preview.BucketKeyChanges[i].ActiveTaskCount = counts[preview.BucketKeyChanges[i].FromKey]
	}
	return preview
}

func (d *Draft) Apply(ctx context.Context, reload func(string) error) (config.Bundle, error) {
	if !d.dirty {
		return CloneBundle(d.original), nil
	}
	if d.blocked != "" {
		return config.Bundle{}, d.errf("tui.studio.err.apply_blocked", "studio draft apply blocked: %s", d.blocked)
	}
	if err := d.validateCandidate(); err != nil {
		return config.Bundle{}, err
	}
	if err := d.ensureCurrentBaseline(); err != nil {
		return config.Bundle{}, err
	}
	original := CloneBundle(d.original)
	candidate := CloneBundle(d.candidate)
	applied, err := d.writeCandidate(ctx, original, candidate)
	if err != nil {
		return config.Bundle{}, err
	}
	if reload == nil {
		d.commitApplied(applied)
		return applied, nil
	}
	if err := reload(d.editor.Path()); err != nil {
		return config.Bundle{}, err
	}
	d.commitApplied(applied)
	return applied, nil
}

func (d *Draft) writeCandidate(ctx context.Context, original, candidate config.Bundle) (config.Bundle, error) {
	return d.editor.Apply(ctx, d.plan, d.baselineHashes, func(bundle *config.Bundle) error {
		if d.editor.Path() != d.sourcePath || !reflect.DeepEqual(*bundle, original) {
			return d.errf("tui.studio.err.baseline_changed", "studio draft baseline changed on disk; reload Studio before applying")
		}
		*bundle = CloneBundle(candidate)
		return nil
	})
}

func (d *Draft) commitApplied(applied config.Bundle) {
	d.original = CloneBundle(applied)
	d.candidate = CloneBundle(applied)
	d.dirty = false
	if bundle, _, hashes, planErr := d.editor.LoadPlan(); planErr == nil {
		d.plan = CloneBundle(bundle)
		d.baselineHashes = cloneHashes(hashes)
	}
}

func cloneHashes(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for path, hash := range in {
		out[path] = hash
	}
	return out
}

func (d *Draft) validateCandidate() error {
	return config.ValidateBundle(d.candidate, LoadedSkills(d.candidate), LoadedLaws(d.candidate), LoadedPersonas(d.candidate), LoadedTemplates(d.candidate))
}
