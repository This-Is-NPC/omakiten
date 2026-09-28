package bundledraft

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/testfixtures/bundleeditor"
)

func TestStudioDraftMutatesCandidateWithoutWriting(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	report := draft.Mutate(func(bundle *config.Bundle) error {
		bundle.Workflows[0].Buckets = append(bundle.Workflows[0].Buckets, config.Bucket{ID: 3, Key: "review", Name: "Review", Position: 3})
		return nil
	})

	if !report.Dirty {
		t.Fatal("draft should be dirty")
	}
	if store.saves != 0 {
		t.Fatalf("candidate mutation wrote to disk %d times", store.saves)
	}
	if len(store.bundle.Workflows[0].Buckets) != 2 {
		t.Fatalf("store bundle mutated before apply")
	}
	assertContains(t, report.DiffSummary, "Bucket added: review")
}

func TestStudioDraftValidationBlocksInvalidCandidates(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	report := draft.Mutate(func(bundle *config.Bundle) error {
		bundle.Workflows[0].Buckets[1].Key = bundle.Workflows[0].Buckets[0].Key
		return nil
	})

	if report.ValidationError == nil || !strings.Contains(report.ValidationError.Error(), "duplicated key") {
		t.Fatalf("validation error = %v, want duplicated key", report.ValidationError)
	}
	if _, err := draft.Apply(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "duplicated key") {
		t.Fatalf("Apply error = %v, want duplicated key", err)
	}
	if store.saves != 0 {
		t.Fatalf("invalid apply wrote to disk %d times", store.saves)
	}
}

func TestStudioDraftValidationUsesMCPCommandSkillSubset(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	report := draft.Mutate(func(bundle *config.Bundle) error {
		bundle.MCPCommands["task"] = config.MCPCommandSpec{Persona: "builder", Skills: []string{"deploy"}}
		return nil
	})

	if report.ValidationError == nil || !strings.Contains(report.ValidationError.Error(), "not in persona") {
		t.Fatalf("validation error = %v, want skill subset error", report.ValidationError)
	}
}

func TestStudioDraftMCPCommandLawConflictBlocksCandidate(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	draft, err := New(bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml")))
	if err != nil {
		t.Fatal(err)
	}

	report := draft.SetMCPCommandSpec("task", config.MCPCommandSpec{Persona: "builder", Laws: []string{"safety"}, LawsDisabled: []string{"safety"}, Skills: []string{"code"}})

	if report.ValidationError == nil || !strings.Contains(report.ValidationError.Error(), "both laws and laws_disabled") {
		t.Fatalf("validation error = %v, want law conflict", report.ValidationError)
	}
}

func TestStudioDraftMCPCommandTemplateOrderingAndApply(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Templates = []config.TaskTemplate{{Slug: "requirements"}, {Slug: "acceptance"}}
	bundle.AllTemplates = []config.TaskTemplate{{Slug: "requirements"}, {Slug: "acceptance"}}
	store := &studioDraftStore{bundle: bundle}
	draft, err := New(bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml")))
	if err != nil {
		t.Fatal(err)
	}

	report := draft.SetMCPCommandSpec("task", config.MCPCommandSpec{Persona: "builder", Skills: []string{"code"}, Templates: []string{"acceptance", "requirements"}})
	if report.ValidationError != nil {
		t.Fatalf("set command validation error = %v", report.ValidationError)
	}
	if got := strings.Join(report.Candidate.MCPCommands["task"].Templates, ","); got != "acceptance,requirements" {
		t.Fatalf("template order = %q, want acceptance,requirements", got)
	}
	if _, err := draft.Apply(context.Background(), nil); err != nil {
		t.Fatalf("Apply error = %v", err)
	}
	if store.saves != 1 {
		t.Fatalf("saves = %d, want 1", store.saves)
	}
	if got := strings.Join(store.bundle.MCPCommands["task"].Templates, ","); got != "acceptance,requirements" {
		t.Fatalf("saved template order = %q, want acceptance,requirements", got)
	}
}

func TestStudioDraftApplyKeepsPublishedCandidateWhenReloadFails(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	editor := bundleeditor.New(store, path)
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}
	draft.Mutate(func(bundle *config.Bundle) error {
		bundle.Workflows[0].Buckets[0].Name = "Inbox"
		return nil
	})
	reloadCalls := 0
	reloadErr := errors.New("reload failed")

	_, err = draft.Apply(context.Background(), func(string) error {
		reloadCalls++
		if reloadCalls == 1 {
			return reloadErr
		}
		return nil
	})

	if !errors.Is(err, reloadErr) {
		t.Fatalf("Apply error = %v, want reload failure", err)
	}
	if reloadCalls != 1 {
		t.Fatalf("reload calls = %d, want candidate only", reloadCalls)
	}
	if got := store.bundle.Workflows[0].Buckets[0].Name; got != "Inbox" {
		t.Fatalf("store bucket name = %q, want published candidate", got)
	}
	if store.saves != 1 {
		t.Fatalf("saves = %d, want candidate only", store.saves)
	}
}

func TestStudioDraftBlocksImportedBundles(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.SourcePaths = []string{"/cfg/omakiten.yaml", "/cfg/workflows.yaml"}
	store := &studioDraftStore{bundle: bundle}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}
	draft.Mutate(func(bundle *config.Bundle) error {
		bundle.Workflows[0].Buckets[0].Name = "Inbox"
		return nil
	})
	report := draft.Report()

	if report.BlockedReason == "" || !strings.Contains(report.BlockedReason, "imported config blocks") {
		t.Fatalf("blocked reason = %q", report.BlockedReason)
	}
	if _, err := draft.Apply(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("Apply error = %v, want blocked", err)
	}
	if store.saves != 0 {
		t.Fatalf("blocked apply wrote to disk %d times", store.saves)
	}
}

func TestStudioDraftImpactPreviewReportsBucketChangesAndCounts(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}
	draft.Mutate(func(bundle *config.Bundle) error {
		bundle.Workflows[0].Buckets[0].Key = "inbox"
		bundle.Workflows[0].Buckets = bundle.Workflows[0].Buckets[:1]
		bundle.Workflows[0].Buckets[0].Position = 2
		return nil
	})

	preview := draft.ImpactPreview(context.Background(), studioDraftCounter{counts: map[string]int{"backlog": 4, "done": 2}})

	if len(preview.RemovedBuckets) != 1 || preview.RemovedBuckets[0].Key != "done" || preview.RemovedBuckets[0].ActiveTaskCount != 2 {
		t.Fatalf("removed buckets = %#v", preview.RemovedBuckets)
	}
	if len(preview.BucketKeyChanges) != 1 || preview.BucketKeyChanges[0].FromKey != "backlog" || preview.BucketKeyChanges[0].ToKey != "inbox" || preview.BucketKeyChanges[0].ActiveTaskCount != 4 {
		t.Fatalf("key changes = %#v", preview.BucketKeyChanges)
	}
	if preview.FinalBucketChanged == nil || preview.FinalBucketChanged.FromKey != "done" || preview.FinalBucketChanged.ToKey != "inbox" {
		t.Fatalf("final change = %#v", preview.FinalBucketChanged)
	}
}

func TestStudioDraftChangeBucketKeyUpdatesReferences(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Config.Views.Table.Filter.Bucket = []string{"backlog", "done"}
	bundle.Workflows[0].Transitions[0].Guards = []config.TransitionGuard{{Type: "blockers_in", Buckets: []string{"backlog", "done"}}}
	store := &studioDraftStore{bundle: bundle}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	report := draft.ChangeBucketKey(1, "inbox")

	if report.ValidationError != nil {
		t.Fatalf("validation error = %v", report.ValidationError)
	}
	workflow := report.Candidate.Workflows[0]
	if got := workflow.Buckets[0].Key; got != "inbox" {
		t.Fatalf("bucket key = %q, want inbox", got)
	}
	if got := workflow.Transitions[0].Guards[0].Buckets[0]; got != "inbox" {
		t.Fatalf("guard bucket = %q, want inbox", got)
	}
	if got := report.Candidate.Config.Views.Table.Filter.Bucket[0]; got != "inbox" {
		t.Fatalf("table bucket filter = %q, want inbox", got)
	}
	if workflow.Transitions[0].From != 1 || workflow.Transitions[0].To != 2 {
		t.Fatalf("transition ids changed: %#v", workflow.Transitions[0])
	}
	assertContains(t, report.DiffSummary, "Bucket key changed: backlog -> inbox")
}

func TestStudioDraftAddBucketGeneratesNextID(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	report := draft.AddBucket("review", "Review")

	if report.ValidationError != nil {
		t.Fatalf("validation error = %v", report.ValidationError)
	}
	added := report.Candidate.Workflows[0].Buckets[2]
	if added.ID != 3 || added.Position != 3 || added.Key != "review" {
		t.Fatalf("added bucket = %#v, want id 3 position 3 key review", added)
	}
	if len(report.Candidate.Workflows[0].Transitions) != 1 {
		t.Fatalf("add bucket should not create transitions: %#v", report.Candidate.Workflows[0].Transitions)
	}
	assertContains(t, report.DiffSummary, "Bucket added: review")
}

func TestStudioDraftDeleteBucketBlocksActiveTasksAndRemovesSafeReferences(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	blocked := draft.DeleteBucket(context.Background(), studioDraftCounter{counts: map[string]int{"done": 2}}, 2)
	if blocked.ValidationError == nil || !strings.Contains(blocked.ValidationError.Error(), "active task") {
		t.Fatalf("delete error = %v, want active task block", blocked.ValidationError)
	}
	if len(blocked.Candidate.Workflows[0].Buckets) != 2 {
		t.Fatalf("blocked delete mutated candidate")
	}

	safe := draft.DeleteBucket(context.Background(), studioDraftCounter{counts: map[string]int{"done": 0}}, 2)
	if safe.ValidationError != nil {
		t.Fatalf("validation error = %v", safe.ValidationError)
	}
	workflow := safe.Candidate.Workflows[0]
	if len(workflow.Buckets) != 1 || workflow.Buckets[0].Key != "backlog" {
		t.Fatalf("buckets after delete = %#v", workflow.Buckets)
	}
	if len(workflow.Transitions) != 0 {
		t.Fatalf("deleted bucket transition survived: %#v", workflow.Transitions)
	}
	assertContains(t, safe.DiffSummary, "Bucket removed: done")
}

func TestStudioDraftMoveBucketSurfacesFinalBucketWarning(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	report := draft.MoveBucket(1, 1)
	preview := draft.ImpactPreview(context.Background(), nil)

	if report.ValidationError != nil {
		t.Fatalf("validation error = %v", report.ValidationError)
	}
	if preview.FinalBucketChanged == nil || preview.FinalBucketChanged.FromKey != "done" || preview.FinalBucketChanged.ToKey != "backlog" {
		t.Fatalf("final bucket warning = %#v", preview.FinalBucketChanged)
	}
	assertContains(t, report.DiffSummary, "Buckets reordered")
}

func TestStudioDraftSetBucketPermissions(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	draft.SetBucketPermission(1, BucketPermissionTask, BucketPermissionEdit, false)
	report := draft.SetBucketPermission(1, BucketPermissionComment, BucketPermissionCreate, false)

	if report.ValidationError != nil {
		t.Fatalf("validation error = %v", report.ValidationError)
	}
	perms := report.Candidate.Workflows[0].Buckets[0].Permissions
	if perms == nil || perms.Task == nil || perms.Task.Edit == nil || perms.Task.Edit.Allow == nil || *perms.Task.Edit.Allow {
		t.Fatalf("task edit permission not set false: %#v", perms)
	}
	if perms.Comment == nil || perms.Comment.Create == nil || perms.Comment.Create.Allow == nil || *perms.Comment.Create.Allow {
		t.Fatalf("comment create permission not set false: %#v", perms)
	}
	assertContains(t, report.DiffSummary, "Bucket permissions changed: backlog")
}

func TestStudioDraftDiffCoversCombinedBucketAndFilterWrites(t *testing.T) {
	draft, err := New(bundleeditor.New(&studioDraftStore{bundle: studioDraftBundle()}, filepath.Join(t.TempDir(), "omakiten.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	report := draft.Mutate(func(bundle *config.Bundle) error {
		bundle.Config.Views.Table.Filter.Bucket = []string{"inbox"}
		bundle.Workflows[0].Buckets[0].Key = "inbox"
		bundle.Workflows[0].Buckets[0].Name = "Inbox"
		return nil
	})

	for _, want := range []string{
		"Bucket key changed: backlog -> inbox",
		"Bucket renamed: Backlog -> Inbox",
		"Table bucket filter changed: none -> inbox",
	} {
		assertContains(t, report.DiffSummary, want)
	}
	if report.Dirty && len(report.DiffSummary) == 1 && report.DiffSummary[0] == "No changes" {
		t.Fatal("dirty report claimed No changes")
	}
}

func TestStudioDraftReportDeepClonesGuardBuckets(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Workflows[0].Transitions[0].Guards = []config.TransitionGuard{{Type: "blockers_in", Buckets: []string{"backlog"}}}
	draft, err := New(bundleeditor.New(&studioDraftStore{bundle: bundle}, filepath.Join(t.TempDir(), "omakiten.yaml")))
	if err != nil {
		t.Fatal(err)
	}

	report := draft.Report()
	report.Candidate.Workflows[0].Transitions[0].Guards[0].Buckets[0] = "owned"
	report.Original.Workflows[0].Transitions[0].Guards[0].Buckets[0] = "owned"
	next := draft.Report()
	if got := next.Candidate.Workflows[0].Transitions[0].Guards[0].Buckets[0]; got != "backlog" {
		t.Fatalf("candidate guard bucket aliased through report: %q", got)
	}
	if got := next.Original.Workflows[0].Transitions[0].Guards[0].Buckets[0]; got != "backlog" {
		t.Fatalf("original guard bucket aliased through report: %q", got)
	}
}

func TestCloneBundleIsolatesAllNestedMutableFields(t *testing.T) {
	bundle := cloneNestedMutableBundle()
	clone := CloneBundle(bundle)
	clone.Skills[0].RoleAffinity[0] = "owned"
	clone.AllSkills[0].RoleAffinity[0] = "owned"
	clone.Projects[0].Laws[0] = "owned"
	notification := clone.Notifications["alert"]
	notification.Animation[0].Value = "owned"
	*notification.Padding.Top = 9
	*notification.AutoHeight = false
	notification.Dismiss.Keys[0] = "owned"
	notification.Actions[0].Arguments["slug"] = "owned"
	clone.Notifications["alert"] = notification
	clone.Languages[0].Keys["key"] = "owned"
	clone.ActiveTheme.Colors["accent"] = "owned"
	clone.Sources["theme.active"] = "owned"
	clone.Config.Hooks[0].Args["nested"].([]string)[0] = "owned"
	clone.SubtaskBundle.Skills[0].RoleAffinity[0] = "owned"
	clone.SubtaskBundle.ActiveTheme.Colors["accent"] = "owned"
	clone.SubtaskBundle.Sources["workflow.active"] = "owned"

	assertNestedMutableBundleIsolated(t, bundle)
}

func cloneNestedMutableBundle() config.Bundle {
	padding := 1
	autoHeight := true
	bundle := studioDraftBundle()
	bundle.Skills[0].RoleAffinity = []string{"implementer"}
	bundle.AllSkills[0].RoleAffinity = []string{"reviewer"}
	bundle.Projects = []config.Project{{Slug: "project", Laws: []string{"project-law"}}}
	bundle.Notifications = map[string]config.Notification{"alert": {
		Animation:  []config.NotificationFrame{{Frame: 1, Value: "frame"}},
		Padding:    &config.NotificationPadding{Top: &padding},
		AutoHeight: &autoHeight,
		Dismiss:    config.NotificationDismiss{Keys: []string{"esc"}},
		Actions:    []config.NotificationAction{{Operation: "skill.get", Arguments: map[string]any{"slug": "okt"}}},
	}}
	bundle.Languages = []config.Language{{Code: "en", Keys: map[string]string{"key": "value"}}}
	bundle.ActiveTheme = config.Theme{Colors: map[string]string{"accent": "blue"}}
	bundle.Sources = map[string]string{"theme.active": config.SourceProject}
	bundle.Config.Hooks = []config.HookSpec{{Args: map[string]interface{}{"nested": []string{"one"}}}}
	bundle.SubtaskBundle = &config.Bundle{
		Skills:      []config.Skill{{Slug: "sub", RoleAffinity: []string{"planner"}}},
		ActiveTheme: config.Theme{Colors: map[string]string{"accent": "green"}},
		Sources:     map[string]string{"workflow.active": config.SourceDefault},
	}
	return bundle
}

func assertNestedMutableBundleIsolated(t *testing.T, bundle config.Bundle) {
	if bundle.Skills[0].RoleAffinity[0] != "implementer" || bundle.AllSkills[0].RoleAffinity[0] != "reviewer" {
		t.Fatal("skill role affinity aliases clone")
	}
	if bundle.Projects[0].Laws[0] != "project-law" {
		t.Fatal("project laws alias clone")
	}
	originalNotification := bundle.Notifications["alert"]
	if originalNotification.Animation[0].Value != "frame" || *originalNotification.Padding.Top != 1 || !*originalNotification.AutoHeight || originalNotification.Dismiss.Keys[0] != "esc" || originalNotification.Actions[0].Arguments["slug"] != "okt" {
		t.Fatal("notification internals alias clone")
	}
	if bundle.Languages[0].Keys["key"] != "value" || bundle.ActiveTheme.Colors["accent"] != "blue" || bundle.Sources["theme.active"] != config.SourceProject {
		t.Fatal("language, theme, or source maps alias clone")
	}
	if bundle.Config.Hooks[0].Args["nested"].([]string)[0] != "one" {
		t.Fatal("interface-held hook args alias clone")
	}
	if bundle.SubtaskBundle.Skills[0].RoleAffinity[0] != "planner" || bundle.SubtaskBundle.ActiveTheme.Colors["accent"] != "green" || bundle.SubtaskBundle.Sources["workflow.active"] != config.SourceDefault {
		t.Fatal("subtask bundle aliases clone")
	}
}

func TestStudioDraftApplyBlocksChangedPathOrBaseline(t *testing.T) {
	tests := map[string]func(*studioDraftStore, Editor){
		"profile path changed": func(_ *studioDraftStore, editor Editor) { editor.SetPath("other.yaml") },
		"on-disk baseline changed": func(store *studioDraftStore, _ Editor) {
			store.bundle.Workflows[0].Buckets[0].Name = "Externally edited"
		},
		"entity catalog changed": func(store *studioDraftStore, _ Editor) {
			store.bundle.AllSkills[0].RoleAffinity = []string{"external"}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			store := &studioDraftStore{bundle: studioDraftBundle()}
			editor := bundleeditor.New(store, "omakiten.yaml")
			draft, err := New(editor)
			if err != nil {
				t.Fatal(err)
			}
			draft.RenameBucket(1, "Inbox")
			change(store, editor)
			if _, err := draft.Apply(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "reload Studio") && !strings.Contains(err.Error(), "reopen Studio") {
				t.Fatalf("Apply error = %v, want stale source/baseline block", err)
			}
			if store.saves != 0 {
				t.Fatalf("stale draft wrote bundle, saves=%d", store.saves)
			}
		})
	}
}

func TestStudioDraftAddTransitionBlocksInvalidPairs(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	added := draft.AddTransition(2, 1)
	if added.ValidationError != nil {
		t.Fatalf("add transition error = %v", added.ValidationError)
	}
	got := added.Candidate.Workflows[0].Transitions[1]
	if got.From != 2 || got.To != 1 || len(got.Guards) != 0 {
		t.Fatalf("added transition = %#v, want id pair 2->1 with no guards", got)
	}
	assertContains(t, added.DiffSummary, "Transition added: done -> backlog")

	duplicate := draft.AddTransition(2, 1)
	if duplicate.ValidationError == nil || !strings.Contains(duplicate.ValidationError.Error(), "already exists") {
		t.Fatalf("duplicate error = %v, want already exists", duplicate.ValidationError)
	}
	self := draft.AddTransition(1, 1)
	if self.ValidationError == nil || !strings.Contains(self.ValidationError.Error(), "self-transitions") {
		t.Fatalf("self error = %v, want self-transitions", self.ValidationError)
	}
	missing := draft.AddTransition(1, 99)
	if missing.ValidationError == nil || !strings.Contains(missing.ValidationError.Error(), "to bucket id 99 not found") {
		t.Fatalf("missing error = %v, want missing bucket", missing.ValidationError)
	}
}

func TestStudioDraftRemoveTransitionDropsGuardedEdgeAndDiffsGuards(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Workflows[0].Transitions[0].Guards = []config.TransitionGuard{{Type: "comments_min", Count: 1}, {Type: "comments_tagged", Tag: "review", Count: 1}}
	store := &studioDraftStore{bundle: bundle}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))
	draft, err := New(editor)
	if err != nil {
		t.Fatal(err)
	}

	removed := draft.RemoveTransition(1, 2)
	if removed.ValidationError != nil {
		t.Fatalf("remove transition error = %v", removed.ValidationError)
	}
	if len(removed.Candidate.Workflows[0].Transitions) != 0 {
		t.Fatalf("removed transition survived: %#v", removed.Candidate.Workflows[0].Transitions)
	}
	assertContains(t, removed.DiffSummary, "Transition removed: backlog -> done")
	assertContains(t, removed.DiffSummary, "Transition guards removed: backlog -> done (2)")

	again := draft.RemoveTransition(1, 2)
	if again.ValidationError == nil || !strings.Contains(again.ValidationError.Error(), "does not exist") {
		t.Fatalf("remove missing error = %v, want does not exist", again.ValidationError)
	}
}

func TestStudioDraftGuardDiffIncludesUsefulDetails(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Workflows[0].Transitions[0].Guards = []config.TransitionGuard{{Type: "comments_min", Count: 1}}
	draft, err := New(bundleeditor.New(&studioDraftStore{bundle: bundle}, filepath.Join(t.TempDir(), "omakiten.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	report := draft.SetGuard(GuardSetTransition, 1, 2, 0, config.TransitionGuard{Type: "comments_tagged", Count: 2, Tag: "review"})
	if got := strings.Join(report.DiffSummary, "\n"); !strings.Contains(got, "comments_min[count=1] -> comments_tagged[count=2,tag=review]") {
		t.Fatalf("guard diff lacks field details: %s", got)
	}
}

func studioDraftBundle() config.Bundle {
	tru := true
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "omakase", Name: "Omakase"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Workflow: config.WorkflowSettings{Active: "omakase"},
			Theme:    config.ThemeSettings{Active: "default"},
			MCP: config.MCPSettings{
				RecentCommentLimit:        5,
				IncludeWorkflowInContinue: &tru,
				CachePrompts:              &tru,
				NextWorkLimit:             5,
				SimilarTaskLimit:          5,
			},
			TUI:              config.TUISettings{TokenBadge: config.TokenBadgeThresholds{YellowAt: 150, RedAt: 400}},
			TemplateDefaults: []string{"task"},
			Priorities: []config.PriorityDefinition{
				{ID: 1, Value: "low"},
				{ID: 2, Value: "normal", Default: true},
				{ID: 3, Value: "high"},
			},
			Severities: []config.SeverityDefinition{
				{ID: 1, Value: "info"},
				{ID: 2, Value: "warning", Default: true},
				{ID: 3, Value: "error"},
			},
			Views: config.ViewSettings{
				Board:        config.BoardViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Table:        config.TableViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Graph:        config.GraphViewSettings{Sort: config.SortSettings{Field: "id", Order: "asc"}},
				Logs:         config.LogsViewSettings{Sort: config.SortSettings{Order: "desc"}, Limit: 50, WindowDays: 30},
				TaskActivity: config.TaskActivityViewSettings{Sort: config.SortSettings{Order: "asc"}},
			},
			SQLite:    config.SQLiteSettings{BusyTimeoutMs: 5000, CacheSizeKB: 1024},
			Solutions: config.SolutionsSettings{DefaultTopLimit: 10, MaxTopLimit: 100},
			Backup:    config.BackupSettings{RetentionCount: 5},
			Events: config.EventsSettings{
				DefaultRecentLimit: 50,
				Defaults:           config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru},
			},
			Search:      config.SearchSettings{Stopwords: []string{"and", "the"}},
			TagSynonyms: map[string]string{"golang": "go"},
		},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "omakase",
			Name: "Omakase",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "done", Name: "Done", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
		Skills:      []config.Skill{{Slug: "code"}, {Slug: "deploy"}},
		AllSkills:   []config.Skill{{Slug: "code"}, {Slug: "deploy"}},
		Personas:    []config.Persona{{Slug: "builder", SkillRepertoire: []string{"code"}}},
		AllPersonas: []config.Persona{{Slug: "builder", SkillRepertoire: []string{"code"}}},
		MCPCommands: map[string]config.MCPCommandSpec{"task": {Persona: "builder", Skills: []string{"code"}}},
		Surfaces:    config.CanonicalSurfaceTable(),
	}
}

type studioDraftStore struct {
	bundle config.Bundle
	saves  int
}

func (s *studioDraftStore) LoadBundle(string) (config.Bundle, error) {
	return CloneBundle(s.bundle), nil
}

func (s *studioDraftStore) LoadBundlePlan(path string) (config.Bundle, map[string]string, error) {
	bundle, err := s.LoadBundle(path)
	if err != nil {
		return config.Bundle{}, nil, err
	}
	hash, err := s.HashFile(path)
	if err != nil {
		return config.Bundle{}, nil, err
	}
	return bundle, map[string]string{path: hash}, nil
}

func (s *studioDraftStore) SaveBundle(_ string, bundle config.Bundle) error {
	s.saves++
	s.bundle = CloneBundle(bundle)
	return nil
}

func (s *studioDraftStore) HashFile(string) (string, error) {
	raw, err := json.Marshal(s.bundle)
	return string(raw), err
}

func (s *studioDraftStore) ValidatePath(root, path string) error {
	return config.ValidatePath(root, path)
}

func (s *studioDraftStore) WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s *studioDraftStore) RemoveFile(path string) error {
	return os.Remove(path)
}

func (s *studioDraftStore) EnsureDefaultFiles(string) error { return nil }

func (s *studioDraftStore) ConfigRootFromYAMLPath(path string) string { return filepath.Dir(path) }

type studioDraftCounter struct{ counts map[string]int }

func (c studioDraftCounter) CountActiveTasksByBucket(_ context.Context, keys []string) (map[string]int, error) {
	out := map[string]int{}
	for _, key := range keys {
		out[key] = c.counts[key]
	}
	return out, nil
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%q not found in %#v", want, values)
}
