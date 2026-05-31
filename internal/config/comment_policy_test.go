package config

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCommentOpPolicyUnmarshalScalarBool proves the polymorphic value still
// accepts a bare bool and maps it to {Allow}, preserving byte-for-byte
// back-compat with every pre-#405 bare-bool comment/task config.
func TestCommentOpPolicyUnmarshalScalarBool(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{in: "create: true", want: true},
		{in: "create: false", want: false},
	} {
		var p EntityPermission
		if err := yaml.Unmarshal([]byte(tc.in), &p); err != nil {
			t.Fatalf("Unmarshal(%q) = %v", tc.in, err)
		}
		if p.Create == nil || p.Create.Allow == nil || *p.Create.Allow != tc.want {
			t.Fatalf("Unmarshal(%q): want allow=%v, got %+v", tc.in, tc.want, p.Create)
		}
		if len(p.Create.RequireTags) != 0 || len(p.Create.DenyTags) != 0 || p.Create.RequireAnyTag != nil {
			t.Fatalf("Unmarshal(%q): scalar bool must not set tag predicates, got %+v", tc.in, p.Create)
		}
	}
}

// TestCommentOpPolicyUnmarshalRuleObject proves the mapping form decodes the
// allow flag plus all three tag predicates.
func TestCommentOpPolicyUnmarshalRuleObject(t *testing.T) {
	const in = `
create:
  allow: true
  require_tags: [needs-review]
  deny_tags: [locked]
  require_any_tag: true
`
	var p EntityPermission
	if err := yaml.Unmarshal([]byte(in), &p); err != nil {
		t.Fatalf("Unmarshal rule object = %v", err)
	}
	if p.Create == nil || p.Create.Allow == nil || !*p.Create.Allow {
		t.Fatalf("allow: got %+v", p.Create)
	}
	if len(p.Create.RequireTags) != 1 || p.Create.RequireTags[0] != "needs-review" {
		t.Fatalf("require_tags: got %+v", p.Create.RequireTags)
	}
	if len(p.Create.DenyTags) != 1 || p.Create.DenyTags[0] != "locked" {
		t.Fatalf("deny_tags: got %+v", p.Create.DenyTags)
	}
	if p.Create.RequireAnyTag == nil || !*p.Create.RequireAnyTag {
		t.Fatalf("require_any_tag: got %+v", p.Create.RequireAnyTag)
	}
}

// TestCommentOpPolicyUnmarshalUnknownKey proves a typo'd rule key fails loudly
// at unmarshal rather than silently resolving to allow.
func TestCommentOpPolicyUnmarshalUnknownKey(t *testing.T) {
	const in = `
create:
  allow: true
  require_tag: [oops]
`
	var p EntityPermission
	err := yaml.Unmarshal([]byte(in), &p)
	if err == nil {
		t.Fatal("unknown rule key = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "unknown comment permission rule key") {
		t.Fatalf("error = %v, want unknown-key message", err)
	}
}

// TestValidatePermissionScopesRejectsTaskTagRules proves task permissions stay
// plain bool: a tag predicate under permissions.task.* is rejected.
func TestValidatePermissionScopesRejectsTaskTagRules(t *testing.T) {
	tr := true
	err := validatePermissionScopes(Workflow{
		Key: "wf",
		Buckets: []Bucket{{
			Key: "done",
			Permissions: &BucketPermissions{
				Task: &EntityPermission{Edit: &CommentOpPolicy{Allow: &tr, DenyTags: []string{"locked"}}},
			},
		}},
	})
	if err == nil {
		t.Fatal("task tag rule = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "only valid on comment permissions") {
		t.Fatalf("error = %v, want comment-only message", err)
	}
}

// TestValidatePermissionScopesRejectsEmptyTagName proves an empty tag name in a
// require/deny list is rejected at validation.
func TestValidatePermissionScopesRejectsEmptyTagName(t *testing.T) {
	err := validatePermissionScopes(Workflow{
		Key: "wf",
		Buckets: []Bucket{{
			Key: "done",
			Permissions: &BucketPermissions{
				Comment: &EntityPermission{Edit: &CommentOpPolicy{RequireTags: []string{"  "}}},
			},
		}},
	})
	if err == nil {
		t.Fatal("empty tag name = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("error = %v, want non-empty message", err)
	}
}

// TestShippedKitsParseCommentPolicyBackCompat is the back-compat regression
// over the shipped kits (#405 acceptance 5): every preset config still
// materializes, loads, validates and snapshots clean after the comment
// permission value became polymorphic. The kits declare only bare-bool comment
// policies (the #389/#404 shapes), so each must round-trip to a CommentOpPolicy
// whose Allow flag matches the original bool and whose Evaluate(nil) preserves
// the pre-#405 verdict — proving bare bools resolve identically.
func TestShippedKitsParseCommentPolicyBackCompat(t *testing.T) {
	for _, preset := range []string{"omakase", "izakaya", "kaiseki", "shokunin"} {
		t.Run(preset, func(t *testing.T) {
			tmp := t.TempDir()
			if err := EnsureDefaultFiles(tmp); err != nil {
				t.Fatalf("EnsureDefaultFiles() = %v", err)
			}
			src := filepath.Join(tmp, "config", preset+".yaml")
			bundle, err := LoadBundle(src)
			if err != nil {
				t.Fatalf("LoadBundle(%s) = %v", preset, err)
			}
			if err := ValidateBundle(bundle, bundle.Skills, bundle.Laws, bundle.Personas, bundle.Templates); err != nil {
				t.Fatalf("ValidateBundle(%s) = %v", preset, err)
			}
			snap := BuildSnapshot(bundle)
			if snap == nil {
				t.Fatalf("BuildSnapshot(%s) = nil", preset)
			}
			// Every comment policy in the shipped kits is a bare bool: assert
			// none accidentally parsed tag predicates, and that the resolved
			// Allow matches the original verdict (back-compat invariant).
			for _, wf := range bundle.Workflows {
				assertBareBoolPolicies(t, wf.Defaults)
				for _, b := range wf.Buckets {
					if b.Permissions == nil {
						continue
					}
					assertBareBoolEntity(t, b.Permissions.Task)
					assertBareBoolEntity(t, b.Permissions.Comment)
				}
			}
		})
	}
}

func assertBareBoolPolicies(t *testing.T, d *WorkflowDefaults) {
	t.Helper()
	if d == nil {
		return
	}
	assertBareBoolEntity(t, d.Task)
	assertBareBoolEntity(t, d.Comment)
}

func assertBareBoolEntity(t *testing.T, p *EntityPermission) {
	t.Helper()
	if p == nil {
		return
	}
	for _, v := range []*CommentOpPolicy{p.Create, p.Edit, p.Delete} {
		if v == nil {
			continue
		}
		if len(v.RequireTags) > 0 || len(v.DenyTags) > 0 || v.RequireAnyTag != nil {
			t.Fatalf("shipped kit declared a tag predicate where a bare bool was expected: %+v", v)
		}
	}
	assertBareBoolEntity(t, p.Task)
	assertBareBoolEntity(t, p.Project)
	assertBareBoolEntity(t, p.Universal)
}
