package domain

import "testing"

// TestCommentOpPolicyEvaluate covers every predicate branch of the polymorphic
// comment-op rule: base allow short-circuit, require_tags (ALL present),
// deny_tags (ANY present), require_any_tag (non-empty set), and the implicit
// allow when undeclared.
func TestCommentOpPolicyEvaluate(t *testing.T) {
	cases := []struct {
		name   string
		policy CommentOpPolicy
		tags   []string
		want   bool
	}{
		{name: "undeclared allows", policy: CommentOpPolicy{}, tags: nil, want: true},
		{name: "allow false denies regardless of tags", policy: CommentOpPolicy{Allow: rawBool(false), RequireTags: []string{"x"}}, tags: []string{"x"}, want: false},
		{name: "allow true bare", policy: CommentOpPolicy{Allow: rawBool(true)}, tags: nil, want: true},
		{name: "require_tags all present", policy: CommentOpPolicy{RequireTags: []string{"a", "b"}}, tags: []string{"a", "b", "c"}, want: true},
		{name: "require_tags missing one denies", policy: CommentOpPolicy{RequireTags: []string{"a", "b"}}, tags: []string{"a"}, want: false},
		{name: "deny_tags any present denies", policy: CommentOpPolicy{DenyTags: []string{"locked"}}, tags: []string{"locked", "x"}, want: false},
		{name: "deny_tags none present allows", policy: CommentOpPolicy{DenyTags: []string{"locked"}}, tags: []string{"x"}, want: true},
		{name: "require_any_tag empty denies", policy: CommentOpPolicy{RequireAnyTag: rawBool(true)}, tags: nil, want: false},
		{name: "require_any_tag with tag allows", policy: CommentOpPolicy{RequireAnyTag: rawBool(true)}, tags: []string{"x"}, want: true},
		{name: "combined allow+require+deny pass", policy: CommentOpPolicy{Allow: rawBool(true), RequireTags: []string{"a"}, DenyTags: []string{"b"}}, tags: []string{"a"}, want: true},
		{name: "combined deny wins", policy: CommentOpPolicy{Allow: rawBool(true), RequireTags: []string{"a"}, DenyTags: []string{"b"}}, tags: []string{"a", "b"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.Evaluate(tc.tags); got != tc.want {
				t.Fatalf("Evaluate(%v) = %v, want %v", tc.tags, got, tc.want)
			}
		})
	}
}

// TestResolveCommentPolicyMostSpecificWins proves the chain returns the first
// declared layer (most-specific wins) and falls to an allowing policy when
// every candidate is nil/undeclared.
func TestResolveCommentPolicyMostSpecificWins(t *testing.T) {
	specific := &CommentOpPolicy{DenyTags: []string{"locked"}}
	outer := &CommentOpPolicy{Allow: rawBool(true)}

	got := resolveCommentPolicy(nil, specific, outer)
	if len(got.DenyTags) != 1 || got.DenyTags[0] != "locked" {
		t.Fatalf("most-specific declared layer should win, got %+v", got)
	}

	// All nil / undeclared → implicit allow.
	got = resolveCommentPolicy(nil, &CommentOpPolicy{}, nil)
	if !got.Evaluate(nil) {
		t.Fatalf("undeclared chain must resolve to allow, got %+v", got)
	}
}

func TestResolveCommentPolicyUnknownOperationDenies(t *testing.T) {
	b := Bucket{Permissions: &BucketPermissions{Comment: &EntityPermission{Edit: boolPtr(true)}}}
	if got := b.ResolveCommentPolicy(nil, "publish"); got.Evaluate(nil) {
		t.Fatalf("unknown bucket comment operation resolved to allow: %+v", got)
	}

	defaults := &WorkflowDefaults{Comment: &EntityPermission{Project: &EntityPermission{Edit: boolPtr(true)}}}
	if got := ResolveCommentScopePolicy(defaults, CommentScopeProject, "publish"); got.Evaluate(nil) {
		t.Fatalf("unknown scoped comment operation resolved to allow: %+v", got)
	}
}
