package agent

import (
	"strings"
	"testing"
)

// TestSetSettingsStoresValuesVerbatim verifies the strict contract:
// SetSettings is a plain assignment now that the validator guarantees
// every MCP field is positive before the composition root reaches
// here. The previous "clamp zero/negative to defaults" behaviour is
// gone — defaults no longer exist in code.
func TestSetSettingsStoresValuesVerbatim(t *testing.T) {
	fixture := newAgentFixture(t)

	fixture.service.SetSettings(ServiceSettings{
		RecentCommentLimit: 7,
		MaxCommentChars:    42,
		RecentContextLimit: 4,
		NextWorkLimit:      6,
		SimilarTaskLimit:   3,
	})
	got := fixture.service.settings
	if got.RecentCommentLimit != 7 || got.MaxCommentChars != 42 ||
		got.RecentContextLimit != 4 || got.NextWorkLimit != 6 || got.SimilarTaskLimit != 3 {
		t.Fatalf("SetSettings did not store values verbatim: %+v", got)
	}
}

// TestTruncateBodyEnforcesMaxChars covers the helper used by shapedRecentComments.
// We exercise three boundaries: short body untouched, body at exactly the cap
// untouched, body past the cap cut at rune boundary with `…` appended.
func TestTruncateBodyEnforcesMaxChars(t *testing.T) {
	cases := []struct {
		name string
		body string
		max  int
		want string
	}{
		{"short body untouched", "hello", 10, "hello"},
		{"exact length untouched", "hello", 5, "hello"},
		{"past cap cut with ellipsis", "hello world", 5, "hello…"},
		{"trailing whitespace trimmed before ellipsis", "hello   world", 6, "hello…"},
		{"unicode rune boundary respected", "olá mundo", 4, "olá…"},
		{"max=0 returns original", "anything goes", 0, "anything goes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateBody(tc.body, tc.max)
			if got != tc.want {
				t.Fatalf("truncateBody(%q, %d) = %q, want %q", tc.body, tc.max, got, tc.want)
			}
		})
	}
}

// TestContinueTaskHonorsIncludeWorkflowSetting verifies the config-driven
// default: when settings.IncludeWorkflow is false and the caller does not
// override, the response carries no workflow block (zero value).
func TestContinueTaskHonorsIncludeWorkflowSetting(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSettings(ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    false,
		CachePrompts:       true,
	})

	resp, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{TaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("ContinueTask() error = %v", err)
	}
	if resp.Workflow.Key != "" || len(resp.Workflow.Buckets) > 0 {
		t.Fatalf("Workflow should be empty when settings.IncludeWorkflow=false, got %+v", resp.Workflow)
	}
}

// TestContinueTaskPerCallIncludeWorkflowOverride verifies that a per-call
// `include_workflow` argument overrides the config default in both directions.
func TestContinueTaskPerCallIncludeWorkflowOverride(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSettings(ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    false, // default off
	})

	// Caller forces workflow ON despite default off.
	on := true
	resp, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{
		TaskID:          fixture.taskA1.ID,
		IncludeWorkflow: &on,
	})
	if err != nil {
		t.Fatalf("ContinueTask() error = %v", err)
	}
	if resp.Workflow.Key == "" {
		t.Fatalf("Workflow should be populated when caller passes include_workflow=true")
	}

	// Caller forces workflow OFF despite default on.
	fixture.service.SetSettings(ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    true, // default on
	})
	off := false
	resp, err = fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{
		TaskID:          fixture.taskA1.ID,
		IncludeWorkflow: &off,
	})
	if err != nil {
		t.Fatalf("ContinueTask() error = %v", err)
	}
	if resp.Workflow.Key != "" || len(resp.Workflow.Buckets) > 0 {
		t.Fatalf("Workflow should be empty when caller passes include_workflow=false")
	}
}

// TestContinueTaskTruncatesCommentBodies verifies the comment-body truncation
// path end-to-end: when MaxCommentChars > 0, every body shipped on the
// response is at most that many chars + the ellipsis suffix.
func TestContinueTaskTruncatesCommentBodies(t *testing.T) {
	fixture := newAgentFixture(t)
	// Seed a long comment.
	long := strings.Repeat("x", 1000)
	if _, err := fixture.store.AddComment(fixture.ctx, fixture.projectA.ID, fixture.taskA1.ID, long, "agent", nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	fixture.service.SetSettings(ServiceSettings{
		RecentCommentLimit: 5,
		MaxCommentChars:    50,
		IncludeWorkflow:    true,
	})

	resp, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{TaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("ContinueTask() error = %v", err)
	}
	if len(resp.Comments) == 0 {
		t.Fatal("Comments empty")
	}
	for _, c := range resp.Comments {
		// Each body either fits the cap as-is or carries the truncation
		// suffix. Length in runes must respect cap+1 (the trailing `…`).
		runeLen := len([]rune(c.Body))
		if runeLen > 51 {
			t.Fatalf("comment body length = %d, want <=51 (cap 50 + ellipsis), body = %q", runeLen, c.Body)
		}
	}
}

// TestSettingsCachePromptsExposed sanity-checks the accessor used by the MCP
// adapter to decide whether to stamp the cache_control hint on prompt content.
func TestSettingsCachePromptsExposed(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSettings(ServiceSettings{CachePrompts: true})
	if !fixture.service.SettingsCachePrompts() {
		t.Fatal("SettingsCachePrompts() = false, want true")
	}
	fixture.service.SetSettings(ServiceSettings{CachePrompts: false})
	if fixture.service.SettingsCachePrompts() {
		t.Fatal("SettingsCachePrompts() = true, want false")
	}
}
