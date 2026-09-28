package studio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	yaml "gopkg.in/yaml.v3"

	"omakiten/defaults"
	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	studioprojection "omakiten/internal/studioprojection"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// studioDisplayConfigPath is the config path the fixture DISPLAYS. Studio
// surfaces print Deps.ConfigPath verbatim, and the bundle behind these fixtures
// lives in a temporary directory whose name changes on every run and whose
// length changes the column at which the value wraps — so recording the real
// path would make the fixture differ between two runs on the same machine, let
// alone between two machines.
//
// Deps carries the displayed path separately from the editor that reads the
// file, so the fixture pins the display and lets the editor keep the real
// temporary path. The value is deliberately long enough to wrap inside the
// 80-column value column, because the wrap is part of what the migration must
// preserve.
const studioDisplayConfigPath = "/home/omakiten/.local/share/omakiten/config/omakase.yaml"

// FixtureCase is one recorded Studio state. Keys are replayed through the real
// Update path, bound between keystrokes exactly as the host binds, so the
// recorded state is one a user can actually reach.
type FixtureCase struct {
	Name string

	// PreludeID and PreludeKeys drive a DIFFERENT sub-screen first, on the same
	// Screen value. That is how the host hands an edited candidate across a
	// sub-tab switch — Bind keeps the draft the session opened.
	PreludeID   screenhost.ID
	PreludeKeys []string

	ID   screenhost.ID
	Keys []string

	// The properties the recorded state must actually have. Goldens assert them
	// on every run, including a -update run, so no fixture can be refreshed
	// into an empty screen.
	WantScroll         bool
	WantCursor         bool
	WantDirty          bool
	WantInspectorFocus bool
}

// studioGoldenBundle is studioBenchBundle with a guard catalog on top.
//
// The benchmark bundle carries no transition or operation guards, which would
// leave Workflow recording empty guard rows. The guards added here cover every
// field a guard inspector can print (buckets, count, tag, hint) and every set
// kind Workflow lists (three transitions plus archive/delete/unarchive
// operations), so the recorded inspector exercises variable guard payloads.
func studioGoldenBundle() config.Bundle {
	bundle := studioBenchBundle()
	workflow := bundle.Workflows[0]
	workflow.Transitions = []config.Transition{
		{From: 1, To: 2, Guards: []config.TransitionGuard{
			{Type: "comments_min", Count: 1, Hint: "record why the task is being picked up before moving it into development"},
		}},
		{From: 2, To: 3, Guards: []config.TransitionGuard{
			{Type: "comments_tagged", Count: 1, Tag: "tests-passing", Hint: "attach the test command, an output snippet and a duration"},
			{Type: "subtasks_complete"},
		}},
		{From: 3, To: 4, Guards: []config.TransitionGuard{
			{Type: "comments_tagged", Count: 1, Tag: "resume"},
			{Type: "blockers_in", Buckets: []string{"done"}},
			{Type: "wave_gate"},
		}},
	}
	workflow.Operations = config.WorkflowOperations{
		Archive: config.OperationPolicy{Guards: []config.TransitionGuard{
			{Type: "comments_min", Count: 2, Hint: "an archived task keeps its trail; say what happened to it"},
		}},
		Delete: config.OperationPolicy{Guards: []config.TransitionGuard{
			{Type: "blockers_in", Buckets: []string{"done"}},
		}},
		Unarchive: config.OperationPolicy{Guards: []config.TransitionGuard{
			{Type: "comments_tagged", Count: 1, Tag: "reopen"},
		}},
	}
	bundle.Workflows[0] = workflow
	return bundle
}

// writeStudioGoldenTheme puts the bundle's active theme on disk, where a
// shipped project keeps it.
//
// This is not cosmetic. When themes/<active>.yaml is missing the loader records
// a bundle warning that names BOTH candidate theme paths, and the apply overlay
// prints those warnings — so an unthemed fixture writes the recording host's
// temporary directory straight into the golden. The theme's colours never reach
// the render: the screen is styled from the frame the harness builds.
func writeStudioGoldenTheme(configPath string) error {
	path := filepath.Join(config.ConfigRootFromYAMLPath(configPath), "themes", "default.yaml")
	body := "version: 1\nkey: default\nname: Default\ncolors:\n  accent: \"#8AADF4\"\n  border: \"#494D64\"\n"
	if err := config.WriteAtomic(path, []byte(body)); err != nil {
		return fmt.Errorf("write active theme: %w", err)
	}
	return nil
}

// galleryStudioRoot is the on-disk bundle the gallery reuses across Drive
// calls. Goldens pass an explicit root (tb.TempDir) and never touch this.
var (
	galleryStudioRootOnce sync.Once
	galleryStudioRoot     string
	galleryStudioRootErr  error

	galleryStudioWorkflowRootOnce sync.Once
	galleryStudioWorkflowRoot     string
	galleryStudioWorkflowRootErr  error

	galleryStudioPersonasRootOnce sync.Once
	galleryStudioPersonasRoot     string
	galleryStudioPersonasRootErr  error

	galleryStudioHooksRootOnce sync.Once
	galleryStudioHooksRoot     string
	galleryStudioHooksRootErr  error
)

// StudioDeps wires the Studio screen the way the host does: a file-backed
// editor over a materialised bundle, plus the snapshot and catalog the host
// resolves labels and guard hints through. An empty root reuses a process-wide
// gallery fixture directory (bundle written once) but always opens a fresh
// editor so a dirty Drive cannot leak into the next. Goldens pass tb.TempDir()
// so the caller can prove the on-disk root never reaches a fixture.
func StudioDeps(root string) (Deps, error) {
	if root == "" {
		galleryStudioRootOnce.Do(func() {
			galleryStudioRoot, galleryStudioRootErr = materializeStudioRoot("")
		})
		if galleryStudioRootErr != nil {
			return Deps{}, galleryStudioRootErr
		}
		root = galleryStudioRoot
	} else if err := writeStudioBundle(root); err != nil {
		return Deps{}, err
	}
	return openStudioDeps(root)
}

// materializeStudioRoot allocates root (or a temp dir when empty) and writes
// the golden bundle into it.
func materializeStudioRoot(root string) (string, error) {
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "omakiten-studio-*")
		if err != nil {
			return "", fmt.Errorf("mkdir temp: %w", err)
		}
	}
	if err := writeStudioBundle(root); err != nil {
		return "", err
	}
	return root, nil
}

func writeStudioBundle(root string) error {
	return writeStudioBundleOf(root, studioGoldenBundle())
}

func writeStudioBundleOf(root string, bundle config.Bundle) error {
	configPath := filepath.Join(root, "config", "omakase.yaml")
	if err := config.SaveFullBundle(configPath, bundle); err != nil {
		return fmt.Errorf("SaveFullBundle: %w", err)
	}
	if err := writeStudioBenchEntities(configPath, bundle); err != nil {
		return err
	}
	if err := writeStudioGoldenTheme(configPath); err != nil {
		return err
	}
	return nil
}

func openStudioDeps(root string) (Deps, error) {
	return openStudioDepsOf(root, studioGoldenBundle(), studioBenchTasks(37))
}

func openStudioDepsOf(root string, bundle config.Bundle, tasks []domain.Task) (Deps, error) {
	configPath := filepath.Join(root, "config", "omakase.yaml")
	editor := newFixtureBundleEditor(configPath)
	if _, err := editor.Load(); err != nil {
		return Deps{}, fmt.Errorf("editor.Load: %w", err)
	}
	catalog, err := screenfixture.Catalog()
	if err != nil {
		return Deps{}, err
	}
	snapshot := config.BuildSnapshot(bundle)
	return Deps{
		Ctx:        context.Background(),
		Editor:     editor,
		OpenDraft:  openFixtureBundleDraft,
		Snapshot:   snapshot,
		Catalog:    catalog,
		Workflow:   snapshot.Workflow(),
		Tasks:      tasks,
		ConfigPath: studioDisplayConfigPath,
	}, nil
}

// studioWorkflowGoldenBundle overlays the shipped omakase transition shape on
// the shared golden bundle so Workflow goldens match the SVG list.
func studioWorkflowGoldenBundle() config.Bundle {
	bundle := studioGoldenBundle()
	if len(bundle.Workflows) == 0 {
		return bundle
	}
	workflow := bundle.Workflows[0]
	workflow.Transitions = []config.Transition{
		{From: 1, To: 2, Guards: []config.TransitionGuard{
			{Type: "comments_tagged", Count: 1, Tag: "self-branch", Hint: "record the branch before moving into development"},
			{Type: "blockers_in", Buckets: []string{"done"}, Hint: "Every blocker must be in Done before development starts."},
			{Type: "wave_gate"},
		}},
		{From: 2, To: 3, Guards: []config.TransitionGuard{
			{Type: "comments_tagged", Count: 1, Tag: "resume", Hint: "Add a comment tagged #resume summarizing what was implemented, decisions, and open questions."},
			{Type: "comments_tagged", Count: 1, Tag: "tests-passing", Hint: "attach the test command, an output snippet and a duration"},
			{Type: "subtasks_complete"},
		}},
		{From: 3, To: 4, Guards: []config.TransitionGuard{
			{Type: "comments_tagged", Count: 1, Tag: "documentation"},
		}},
		{From: 3, To: 2},
		{From: 2, To: 1},
		{From: 3, To: 1},
		{From: 4, To: 3},
		{From: 4, To: 2},
		{From: 4, To: 1},
	}
	workflow.Operations = config.WorkflowOperations{
		Archive: config.OperationPolicy{Guards: []config.TransitionGuard{
			{Type: "comments_tagged", Count: 1, Tag: "documentation"},
		}},
	}
	bundle.Workflows[0] = workflow
	resume := config.TaskTemplate{Slug: "comment-resume", Name: "Comment Resume", Entity: "comment", Body: commentResumeTemplateBody}
	bundle.Templates = append(bundle.Templates, resume)
	bundle.AllTemplates = append(bundle.AllTemplates, resume)
	return bundle
}

func studioWorkflowTasks() []domain.Task {
	tasks := []domain.Task{
		{ID: 8, Title: "Tiny", BucketKey: "backlog"},
		{ID: 9, Title: "Carry the selection", BucketKey: "dev"},
	}
	id := int64(100)
	pad := func(n int, bucket string) {
		for i := 0; i < n; i++ {
			tasks = append(tasks, domain.Task{ID: id, Title: fmt.Sprintf("Task %d", id), BucketKey: bucket})
			id++
		}
	}
	pad(9, "backlog")
	pad(8, "dev")
	pad(2, "review")
	pad(1, "done")
	return tasks
}

// StudioWorkflowDeps is StudioDeps with the omakase-shaped workflow overlay
// used by studio.workflow goldens and layout tests.
func StudioWorkflowDeps(root string) (Deps, error) {
	if root == "" {
		galleryStudioWorkflowRootOnce.Do(func() {
			var err error
			galleryStudioWorkflowRoot, err = os.MkdirTemp("", "omakiten-studio-workflow-*")
			if err != nil {
				galleryStudioWorkflowRootErr = fmt.Errorf("mkdir temp: %w", err)
				return
			}
			galleryStudioWorkflowRootErr = writeStudioBundleOf(galleryStudioWorkflowRoot, studioWorkflowGoldenBundle())
		})
		if galleryStudioWorkflowRootErr != nil {
			return Deps{}, galleryStudioWorkflowRootErr
		}
		root = galleryStudioWorkflowRoot
	} else if err := writeStudioBundleOf(root, studioWorkflowGoldenBundle()); err != nil {
		return Deps{}, err
	}
	return openStudioDepsOf(root, studioWorkflowGoldenBundle(), studioWorkflowTasks())
}

func workflowFocusKeys(pred func(studioprojection.WorkflowRow) bool) []string {
	idx := WorkflowIndexFor(studioWorkflowGoldenBundle().Workflows[0], pred)
	if idx <= 0 {
		return nil
	}
	keys := make([]string, idx)
	for i := range keys {
		keys[i] = "down"
	}
	return keys
}

type narutoKitFile struct {
	Personas []struct {
		Slug            string   `yaml:"slug"`
		SchemaVersion   int      `yaml:"schema_version"`
		SkillRepertoire []string `yaml:"skill_repertoire"`
	} `yaml:"personas"`
	MCPCommands map[string]config.MCPCommandSpec `yaml:"mcp_commands"`
}

type shippedEntityFM struct {
	Name            string   `yaml:"name"`
	Description     string   `yaml:"description"`
	SchemaVersion   int      `yaml:"schema_version"`
	SkillRepertoire []string `yaml:"skill_repertoire"`
	Laws            []string `yaml:"laws"`
	Severity        string   `yaml:"severity"`
}

func loadShippedMarkdown(path string) (shippedEntityFM, string, error) {
	raw, err := defaults.FS.ReadFile(path)
	if err != nil {
		return shippedEntityFM{}, "", err
	}
	fm, body, err := config.SplitFrontmatter(raw)
	if err != nil {
		return shippedEntityFM{}, "", fmt.Errorf("%s: %w", path, err)
	}
	var meta shippedEntityFM
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return shippedEntityFM{}, "", fmt.Errorf("%s: %w", path, err)
	}
	return meta, string(body), nil
}

func loadNarutoKitFile() (narutoKitFile, error) {
	raw, err := defaults.FS.ReadFile("config/themes/naruto.yaml")
	if err != nil {
		return narutoKitFile{}, err
	}
	var kit narutoKitFile
	if err := yaml.Unmarshal(raw, &kit); err != nil {
		return narutoKitFile{}, fmt.Errorf("naruto.yaml: %w", err)
	}
	if len(kit.Personas) == 0 || len(kit.MCPCommands) == 0 {
		return narutoKitFile{}, fmt.Errorf("naruto.yaml: missing personas or mcp_commands")
	}
	return kit, nil
}

func loadNarutoStudioPersonas(wired []struct {
	Slug            string   `yaml:"slug"`
	SchemaVersion   int      `yaml:"schema_version"`
	SkillRepertoire []string `yaml:"skill_repertoire"`
}) ([]config.Persona, map[string]struct{}, map[string]struct{}, error) {
	personas := make([]config.Persona, 0, len(wired))
	skillSet := map[string]struct{}{}
	lawSet := map[string]struct{}{}
	for _, entry := range wired {
		meta, body, loadErr := loadShippedMarkdown("personas/" + entry.Slug + ".md")
		if loadErr != nil {
			return nil, nil, nil, loadErr
		}
		repertoire := entry.SkillRepertoire
		if len(repertoire) == 0 {
			repertoire = meta.SkillRepertoire
		}
		schema := entry.SchemaVersion
		if schema == 0 {
			schema = meta.SchemaVersion
		}
		personaLaws := meta.Laws
		personas = append(personas, config.Persona{
			Slug:            entry.Slug,
			Name:            meta.Name,
			Description:     meta.Description,
			Body:            body,
			SchemaVersion:   schema,
			SkillRepertoire: append([]string(nil), repertoire...),
			Laws:            append([]string(nil), personaLaws...),
			SourcePath:      "personas/" + entry.Slug + ".md",
		})
		for _, slug := range repertoire {
			skillSet[slug] = struct{}{}
		}
		for _, slug := range personaLaws {
			lawSet[slug] = struct{}{}
		}
	}
	return personas, skillSet, lawSet, nil
}

func loadNarutoStudioEntities(skillSet, lawSet map[string]struct{}) ([]config.Skill, []config.Law, error) {
	skills := make([]config.Skill, 0, len(skillSet))
	for slug := range skillSet {
		meta, body, loadErr := loadShippedMarkdown("skills/" + slug + ".md")
		if loadErr != nil {
			return nil, nil, loadErr
		}
		skills = append(skills, config.Skill{Slug: slug, Name: meta.Name, Description: meta.Description, Body: body, SchemaVersion: meta.SchemaVersion})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Slug < skills[j].Slug })
	laws := make([]config.Law, 0, len(lawSet))
	for slug := range lawSet {
		meta, body, loadErr := loadShippedMarkdown("laws/" + slug + ".md")
		if loadErr != nil {
			return nil, nil, loadErr
		}
		severity := meta.Severity
		if severity == "" {
			severity = "error"
		}
		laws = append(laws, config.Law{Slug: slug, Name: meta.Name, Severity: severity, Body: body, Scope: "global"})
	}
	sort.Slice(laws, func(i, j int) bool { return laws[i].Slug < laws[j].Slug })
	return skills, laws, nil
}

func loadNarutoStudioKit() (personas []config.Persona, skills []config.Skill, laws []config.Law, commands map[string]config.MCPCommandSpec, err error) {
	kit, err := loadNarutoKitFile()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	personas, skillSet, lawSet, err := loadNarutoStudioPersonas(kit.Personas)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, spec := range kit.MCPCommands {
		for _, slug := range spec.Laws {
			lawSet[slug] = struct{}{}
		}
		for _, slug := range spec.LawsDisabled {
			lawSet[slug] = struct{}{}
		}
	}
	skills, laws, err = loadNarutoStudioEntities(skillSet, lawSet)
	return personas, skills, laws, kit.MCPCommands, err
}

func mergeEntitiesBySlug[T any](base []T, extra []T, slug func(T) string) []T {
	seen := map[string]int{}
	out := append([]T(nil), base...)
	for i, item := range out {
		seen[slug(item)] = i
	}
	for _, item := range extra {
		key := slug(item)
		if i, ok := seen[key]; ok {
			out[i] = item
			continue
		}
		seen[key] = len(out)
		out = append(out, item)
	}
	return out
}

// studioPersonasGoldenBundle overlays the shipped naruto roster on the shared
// golden bundle so Personas goldens show kakashi/shikamaru reverse-index data
// without rewriting Commands / Workflow fixtures.
func studioPersonasGoldenBundle() config.Bundle {
	bundle := studioGoldenBundle()
	personas, skills, laws, commands, err := loadNarutoStudioKit()
	if err != nil {
		panic("studio personas golden: " + err.Error())
	}
	bundle.Personas = personas
	bundle.AllPersonas = personas
	bundle.MCPCommands = commands
	bundle.Skills = mergeEntitiesBySlug(nil, skills, func(s config.Skill) string { return s.Slug })
	bundle.AllSkills = bundle.Skills
	bundle.Laws = mergeEntitiesBySlug(bundle.Laws, laws, func(l config.Law) string { return l.Slug })
	bundle.AllLaws = bundle.Laws
	return bundle
}

// StudioPersonasDeps is StudioDeps with the naruto roster overlay used by
// studio.personas goldens and layout tests.
func StudioPersonasDeps(root string) (Deps, error) {
	bundle := studioPersonasGoldenBundle()
	if root == "" {
		galleryStudioPersonasRootOnce.Do(func() {
			var err error
			galleryStudioPersonasRoot, err = os.MkdirTemp("", "omakiten-studio-personas-*")
			if err != nil {
				galleryStudioPersonasRootErr = fmt.Errorf("mkdir temp: %w", err)
				return
			}
			galleryStudioPersonasRootErr = writeStudioBundleOf(galleryStudioPersonasRoot, bundle)
		})
		if galleryStudioPersonasRootErr != nil {
			return Deps{}, galleryStudioPersonasRootErr
		}
		root = galleryStudioPersonasRoot
	} else if err := writeStudioBundleOf(root, bundle); err != nil {
		return Deps{}, err
	}
	return openStudioDepsOf(root, bundle, studioBenchTasks(37))
}

func personasFocusKeys(slug string) []string {
	idx := PersonaIndexFor(studioPersonasGoldenBundle(), slug)
	if idx <= 0 {
		return nil
	}
	keys := make([]string, idx)
	for i := range keys {
		keys[i] = "down"
	}
	return keys
}

func studioHooksGoldenBundle() config.Bundle {
	bundle := studioGoldenBundle()
	hooks := loadOmakaseStudioHooks()
	hooks = append(hooks, studioHooksExecOverlay())
	bundle.Config.Hooks = hooks
	return bundle
}

func loadOmakaseStudioHooks() []config.HookSpec {
	raw, err := defaults.FS.ReadFile("config/omakase.yaml")
	if err != nil {
		panic("studio hooks golden: " + err.Error())
	}
	var parsed struct {
		Config struct {
			Hooks []config.HookSpec `yaml:"hooks"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		panic("studio hooks golden: omakase.yaml: " + err.Error())
	}
	if len(parsed.Config.Hooks) == 0 {
		panic("studio hooks golden: omakase.yaml has no hooks")
	}
	return parsed.Config.Hooks
}

func studioHooksExecOverlay() config.HookSpec {
	return config.HookSpec{
		On: "task.created",
		Do: "exec",
		Args: map[string]interface{}{
			"argv":       []string{"bash", "/home/me/scripts/log-created.sh"},
			"timeout_ms": 3000,
		},
	}
}

func studioHooksGoldenHistory(hooks []config.HookSpec) map[int][]studioprojection.HookExecuted {
	notifyIdx := HookIndexFor(hooks, studioHookIsGuardTaskDelete)
	execIdx := HookIndexFor(hooks, func(spec config.HookSpec) bool { return spec.Do == "exec" })
	return map[int][]studioprojection.HookExecuted{
		notifyIdx: {
			{CreatedAt: "2026-08-13 14:02:11", Success: true, DurationMs: 4, EventType: "guard.violated", TargetEventID: 2457},
			{CreatedAt: "2026-08-13 13:55:02", Success: true, DurationMs: 3, EventType: "guard.violated", TargetEventID: 2401},
			{CreatedAt: "2026-08-13 12:10:44", Success: true, DurationMs: 5, EventType: "guard.violated"},
		},
		execIdx: {
			{CreatedAt: "2026-08-13 14:33:01", Success: false, DurationMs: 3001, EventType: "task.created", Error: "exec bash timed out after 3s: safe \x1b[31mred\x1b]0;owned\a: context deadline exceeded"},
			{CreatedAt: "2026-08-13 14:12:08", Success: true, DurationMs: 12, EventType: "task.created"},
		},
	}
}

func studioHookIsGuardTaskDelete(spec config.HookSpec) bool {
	return spec.On == "guard.violated" && spec.When["operation"] == "task.delete"
}

func writeStudioHookNotifications(root string, hooks []config.HookSpec) error {
	dir := filepath.Join(root, "notifications")
	seen := map[string]struct{}{}
	for _, spec := range hooks {
		slug := strings.TrimSpace(spec.Notification)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		raw, err := defaults.FS.ReadFile("notifications/" + slug + ".yaml")
		if err != nil {
			return fmt.Errorf("studio hooks notification %s: %w", slug, err)
		}
		if err := config.WriteAtomic(filepath.Join(dir, slug+".yaml"), raw); err != nil {
			return fmt.Errorf("write notification %s: %w", slug, err)
		}
	}
	return nil
}

func writeStudioHooksBundle(root string, bundle config.Bundle) error {
	if err := writeStudioBundleOf(root, bundle); err != nil {
		return err
	}
	return writeStudioHookNotifications(root, bundle.Config.Hooks)
}

// StudioHooksDeps is StudioDeps with shipped omakase hooks plus an exec overlay
// and prepared hook history used by studio.hooks goldens and layout tests.
func StudioHooksDeps(root string) (Deps, error) {
	bundle := studioHooksGoldenBundle()
	if root == "" {
		galleryStudioHooksRootOnce.Do(func() {
			var err error
			galleryStudioHooksRoot, err = os.MkdirTemp("", "omakiten-studio-hooks-*")
			if err != nil {
				galleryStudioHooksRootErr = fmt.Errorf("mkdir temp: %w", err)
				return
			}
			galleryStudioHooksRootErr = writeStudioHooksBundle(galleryStudioHooksRoot, bundle)
		})
		if galleryStudioHooksRootErr != nil {
			return Deps{}, galleryStudioHooksRootErr
		}
		root = galleryStudioHooksRoot
	} else if err := writeStudioHooksBundle(root, bundle); err != nil {
		return Deps{}, err
	}
	deps, err := openStudioDepsOf(root, bundle, studioBenchTasks(37))
	if err != nil {
		return Deps{}, err
	}
	deps.HookHistory = studioHooksGoldenHistory(bundle.Config.Hooks)
	return deps, nil
}

func hooksFocusKeys(pred func(config.HookSpec) bool) []string {
	idx := HookIndexFor(studioHooksGoldenBundle().Config.Hooks, pred)
	if idx <= 0 {
		return nil
	}
	keys := make([]string, idx)
	for i := range keys {
		keys[i] = "down"
	}
	return keys
}

func fixtureCases() []FixtureCase {
	return []FixtureCase{
		{
			// Commands list: four steps along the command axis, list still
			// holds screengrid focus so the ▸ COMMANDS kicker is the pin.
			Name:       "commands-list",
			ID:         screenhost.StudioCommands,
			Keys:       []string{"j", "j", "j", "j"},
			WantCursor: true,
		},
		{
			// Commands preview: four steps along the list, then tab onto the
			// PREVIEW leaf. The metadata gridtable is display-only chrome of
			// the right column — not a tab stop and not a field cursor.
			Name:               "commands",
			ID:                 screenhost.StudioCommands,
			Keys:               []string{"j", "j", "j", "j", "tab"},
			WantCursor:         true,
			WantInspectorFocus: true,
		},
		{
			// Workflow list: DEV bucket in the omakase overlay (SVG focus).
			Name: "workflow-bucket",
			ID:   screenhost.StudioWorkflow,
			Keys: workflowFocusKeys(func(row studioprojection.WorkflowRow) bool {
				return row.Kind == workflowRowBucket && row.Bucket.Key == "dev"
			}),
			WantCursor: true,
		},
		{
			// #resume on dev → review (SVG inspector focus, ▸ PREVIEW kicker).
			Name: "workflow-resume",
			ID:   screenhost.StudioWorkflow,
			Keys: append(workflowFocusKeys(func(row studioprojection.WorkflowRow) bool {
				return row.Kind == workflowRowGuard && row.Guard.Tag == "resume"
			}), "tab"),
			WantCursor:         true,
			WantInspectorFocus: true,
		},
		{
			// blockers_in on backlog → dev (SVG inspector focus).
			Name: "workflow-blockers",
			ID:   screenhost.StudioWorkflow,
			Keys: workflowFocusKeys(func(row studioprojection.WorkflowRow) bool {
				return row.Guard.Type == "blockers_in"
			}),
			WantCursor: true,
		},
		{
			Name:               "personas-kakashi",
			ID:                 screenhost.StudioPersonas,
			Keys:               append(personasFocusKeys("kakashi-hatake"), "tab"),
			WantCursor:         true,
			WantInspectorFocus: true,
		},
		{
			Name: "personas-shikamaru",
			ID:   screenhost.StudioPersonas,
			Keys: personasFocusKeys("shikamaru-nara"),
		},
		{
			Name:               "hooks-notify",
			ID:                 screenhost.StudioHooks,
			Keys:               append(hooksFocusKeys(studioHookIsGuardTaskDelete), "tab"),
			WantCursor:         true,
			WantInspectorFocus: true,
		},
		{
			Name:       "hooks-exec",
			ID:         screenhost.StudioHooks,
			Keys:       hooksFocusKeys(func(spec config.HookSpec) bool { return spec.Do == "exec" }),
			WantCursor: true,
		},
		{
			// Overlay apply on Workflow: dirty a permission then ctrl+s. Apply is
			// not a fifth tab — the card paints over the editor that opened it.
			Name: "workflow-apply",
			ID:   screenhost.StudioWorkflow,
			Keys: append(append([]string(nil), workflowFocusKeys(func(row studioprojection.WorkflowRow) bool {
				return row.Kind == workflowRowBucket && row.Bucket.Key == "dev"
			})...), "e", "ctrl+s"),
			WantCursor: true,
			WantDirty:  true,
		},
	}
}

func studioFixtureEnter(screen Screen, id screenhost.ID, deps Deps, frame screenhost.Frame) Screen {
	entered, ok := screen.Bind(id, deps).Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if !ok {
		panic(fmt.Sprintf("studio fixture: Lifecycle(%s) did not carry a studio.Screen", id))
	}
	if !entered.StudioDraftOpen() {
		panic(fmt.Sprintf("studio fixture: entering %s left no draft open; every later render falls back to the snapshot bundle", id))
	}
	return entered
}

func studioFixtureDrive(screen Screen, frame screenhost.Frame, key string) Screen {
	out := screen.Update(frame, screenfixture.Key(key))
	next, ok := out.Screen.(Screen)
	if !ok {
		panic(fmt.Sprintf("studio fixture: Update(%q) carried %T, not a studio.Screen", key, out.Screen))
	}
	return next
}

func studioFixtureReplay(screen Screen, id screenhost.ID, deps Deps, frame screenhost.Frame, keys []string) Screen {
	for _, key := range keys {
		screen = studioFixtureDrive(screen.Bind(id, deps), frame, key)
	}
	return screen
}

func studioFixtureScenario(c FixtureCase) screenfixture.Scenario {
	var (
		boundID   screenhost.ID
		boundDeps Deps
	)
	return screenfixture.Scenario{
		Name: c.Name,
		Build: func(frame screenhost.Frame) screenhost.Screen {
			load := StudioDeps
			switch {
			case strings.HasPrefix(c.Name, "workflow"):
				load = StudioWorkflowDeps
			case strings.HasPrefix(c.Name, "personas"):
				load = StudioPersonasDeps
			case strings.HasPrefix(c.Name, "hooks"):
				load = StudioHooksDeps
			}
			deps, err := load("")
			if err != nil {
				panic("studio fixture: StudioDeps: " + err.Error())
			}
			boundDeps = deps
			boundID = c.ID

			screen := New()
			if c.PreludeID != "" {
				screen = studioFixtureEnter(screen, c.PreludeID, deps, frame)
				screen = studioFixtureReplay(screen, c.PreludeID, deps, frame, c.PreludeKeys)
			}
			screen = studioFixtureEnter(screen, c.ID, deps, frame)
			return screen
		},
		Bind: func(screen screenhost.Screen) screenhost.Screen {
			return screen.(Screen).Bind(boundID, boundDeps)
		},
		Keys: c.Keys,
	}
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	var out []screenfixture.Scenario
	for _, c := range fixtureCases() {
		if c.ID == id {
			out = append(out, studioFixtureScenario(c))
		}
	}
	return out
}

// commentResumeTemplateBody is the shipped comment-resume scaffold, used by
// the workflow golden bundle so the inspector preview is catalog-backed.
// Kept in sync with defaults/templates/comment-resume.md.
const commentResumeTemplateBody = "**Before** —\n**After** —\n\n**Changes**\n| Aspect | Change |\n| --- | --- |\n|  |  |\n\n**Files** — `path/to/file`\n\n**Validation**\n| Scenario | Outcome |\n| --- | --- |\n|  |  |\n\n**Open questions** —\n"

// openFixtureBundleDraft is the fixture's half of the draft port, the same
// choice internal/tui makes for the live screen. Recording a Studio state with
// no draft open would record the snapshot fallback, not Studio.
func openFixtureBundleDraft(editor contract.BundleEditor) (StudioDraft, error) {
	return bundledraft.New(editor)
}
