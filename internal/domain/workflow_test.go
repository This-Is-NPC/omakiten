package domain

import "testing"

// boolPtr wraps a bare bool as a fully-declared CommentOpPolicy pointer — the
// shape EntityPermission's create/edit/delete fields now take. A plain *bool is
// available via rawBool for the few call sites that still need one.
func boolPtr(b bool) *CommentOpPolicy { return &CommentOpPolicy{Allow: &b} }

func rawBool(b bool) *bool { return &b }

func TestResolveTaskPermission(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		bucket   Bucket
		defaults *WorkflowDefaults
		wantEdit bool
		wantDel  bool
	}{
		"no rule anywhere — implicit allow": {
			bucket:   Bucket{},
			defaults: nil,
			wantEdit: true,
			wantDel:  true,
		},
		"workflow defaults deny edit": {
			bucket: Bucket{},
			defaults: &WorkflowDefaults{
				Task: &EntityPermission{Edit: boolPtr(false)},
			},
			wantEdit: false,
			wantDel:  true,
		},
		"bucket override beats defaults": {
			bucket: Bucket{
				Permissions: &BucketPermissions{
					Task: &EntityPermission{Edit: boolPtr(true)},
				},
			},
			defaults: &WorkflowDefaults{
				Task: &EntityPermission{Edit: boolPtr(false)},
			},
			wantEdit: true,
			wantDel:  true,
		},
		"bucket denies delete": {
			bucket: Bucket{
				Permissions: &BucketPermissions{
					Task: &EntityPermission{Delete: boolPtr(false)},
				},
			},
			defaults: nil,
			wantEdit: true,
			wantDel:  false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edit, del := tc.bucket.ResolveTaskPermission(tc.defaults)
			if edit != tc.wantEdit || del != tc.wantDel {
				t.Fatalf("ResolveTaskPermission = (%v, %v), want (%v, %v)", edit, del, tc.wantEdit, tc.wantDel)
			}
		})
	}
}

func TestResolveCommentPermission(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		bucket   Bucket
		defaults *WorkflowDefaults
		wantEdit bool
		wantDel  bool
	}{
		"comment falls back to task at bucket layer": {
			bucket: Bucket{
				Permissions: &BucketPermissions{
					Task: &EntityPermission{Edit: boolPtr(false), Delete: boolPtr(false)},
				},
			},
			defaults: nil,
			wantEdit: false,
			wantDel:  false,
		},
		"explicit comment overrides task at bucket": {
			bucket: Bucket{
				Permissions: &BucketPermissions{
					Task:    &EntityPermission{Edit: boolPtr(false)},
					Comment: &EntityPermission{Edit: boolPtr(true)},
				},
			},
			defaults: nil,
			wantEdit: true,
			wantDel:  true,
		},
		"defaults.comment beats defaults.task when both declared": {
			bucket: Bucket{},
			defaults: &WorkflowDefaults{
				Task:    &EntityPermission{Edit: boolPtr(false)},
				Comment: &EntityPermission{Edit: boolPtr(true)},
			},
			wantEdit: true,
			wantDel:  true,
		},
		"defaults.comment falls back to defaults.task": {
			bucket: Bucket{},
			defaults: &WorkflowDefaults{
				Task: &EntityPermission{Delete: boolPtr(false)},
			},
			wantEdit: true,
			wantDel:  false,
		},
		"bucket.comment edit beats defaults.task delete": {
			bucket: Bucket{
				Permissions: &BucketPermissions{
					Comment: &EntityPermission{Edit: boolPtr(true)},
				},
			},
			defaults: &WorkflowDefaults{
				Task: &EntityPermission{Delete: boolPtr(false)},
			},
			wantEdit: true,
			wantDel:  false,
		},
		// #389 chain: defaults.comment.task.<op> must be consulted in the
		// task-comment bucket path (above the flat defaults.comment).
		"defaults.comment.task denies with no bucket override": {
			bucket: Bucket{},
			defaults: &WorkflowDefaults{
				Comment: &EntityPermission{
					Task: &EntityPermission{Edit: boolPtr(false), Delete: boolPtr(false)},
				},
			},
			wantEdit: false,
			wantDel:  false,
		},
		"defaults.comment.task beats flat defaults.comment": {
			bucket: Bucket{},
			defaults: &WorkflowDefaults{
				Comment: &EntityPermission{
					Edit: boolPtr(true), // flat (legacy) would allow
					Task: &EntityPermission{Edit: boolPtr(false)},
				},
			},
			wantEdit: false,
			wantDel:  true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edit, del := tc.bucket.ResolveCommentPermission(tc.defaults)
			if edit != tc.wantEdit || del != tc.wantDel {
				t.Fatalf("ResolveCommentPermission = (%v, %v), want (%v, %v)", edit, del, tc.wantEdit, tc.wantDel)
			}
		})
	}
}

func TestResolveCommentScopePermission(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		defaults *WorkflowDefaults
		scope    string
		op       string
		want     bool
	}{
		// task scope, flat back-compat: comment: {edit:false} resolves as task.
		"task flat edit false (back-compat)": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{Edit: boolPtr(false)}},
			scope:    CommentScopeTask, op: CommentOpEdit, want: false,
		},
		"task flat delete defaults true (no rule)": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{Edit: boolPtr(false)}},
			scope:    CommentScopeTask, op: CommentOpDelete, want: true,
		},
		// task scope inherits defaults.task when comment is silent.
		"task inherits defaults.task delete": {
			defaults: &WorkflowDefaults{Task: &EntityPermission{Delete: boolPtr(false)}},
			scope:    CommentScopeTask, op: CommentOpDelete, want: false,
		},
		// task scope explicit comment.task sub-block beats flat + defaults.task.
		"task sub-block beats flat": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{
				Edit: boolPtr(false),
				Task: &EntityPermission{Edit: boolPtr(true)},
			}},
			scope: CommentScopeTask, op: CommentOpEdit, want: true,
		},
		// project scope: explicit allow.
		"project edit allowed": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{
				Project: &EntityPermission{Edit: boolPtr(true), Delete: boolPtr(false)},
			}},
			scope: CommentScopeProject, op: CommentOpEdit, want: true,
		},
		"project delete denied": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{
				Project: &EntityPermission{Edit: boolPtr(true), Delete: boolPtr(false)},
			}},
			scope: CommentScopeProject, op: CommentOpDelete, want: false,
		},
		// project scope has no task inheritance — defaults.task is ignored.
		"project ignores defaults.task": {
			defaults: &WorkflowDefaults{Task: &EntityPermission{Edit: boolPtr(false)}},
			scope:    CommentScopeProject, op: CommentOpEdit, want: true,
		},
		// universal scope.
		"universal edit denied": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{
				Universal: &EntityPermission{Edit: boolPtr(false)},
			}},
			scope: CommentScopeUniversal, op: CommentOpEdit, want: false,
		},
		"universal delete implicit true": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{
				Universal: &EntityPermission{Edit: boolPtr(false)},
			}},
			scope: CommentScopeUniversal, op: CommentOpDelete, want: true,
		},
		// no defaults block — implicit allow everywhere.
		"nil defaults task edit": {
			defaults: nil, scope: CommentScopeTask, op: CommentOpEdit, want: true,
		},
		"nil defaults project delete": {
			defaults: nil, scope: CommentScopeProject, op: CommentOpDelete, want: true,
		},
		"nil defaults universal edit": {
			defaults: nil, scope: CommentScopeUniversal, op: CommentOpEdit, want: true,
		},
		// empty scope falls back to task chain.
		"empty scope = task": {
			defaults: &WorkflowDefaults{Comment: &EntityPermission{Edit: boolPtr(false)}},
			scope:    "", op: CommentOpEdit, want: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveCommentScopePermission(tc.defaults, tc.scope, tc.op); got != tc.want {
				t.Fatalf("ResolveCommentScopePermission(%v, %q, %q) = %v, want %v", tc.defaults, tc.scope, tc.op, got, tc.want)
			}
		})
	}
}

func TestResolveBoolDirectly(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   []*CommentOpPolicy
		want bool
	}{
		"all nil — implicit true":     {in: []*CommentOpPolicy{nil, nil}, want: true},
		"first declared wins (false)": {in: []*CommentOpPolicy{boolPtr(false), boolPtr(true)}, want: false},
		"first declared wins (true)":  {in: []*CommentOpPolicy{nil, boolPtr(true), boolPtr(false)}, want: true},
		"single nil — implicit true":  {in: []*CommentOpPolicy{nil}, want: true},
		"empty — implicit true":       {in: nil, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := resolvePolicyBool(tc.in...); got != tc.want {
				t.Fatalf("resolvePolicyBool(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFinalBucketKey(t *testing.T) {
	w := Workflow{Buckets: []Bucket{
		{Key: "backlog", Position: 1},
		{Key: "dev", Position: 2},
		{Key: "review", Position: 3},
		{Key: "done", Position: 4},
	}}
	if got := w.FinalBucketKey(); got != "done" {
		t.Fatalf("FinalBucketKey = %q, want \"done\"", got)
	}

	if got := (Workflow{}).FinalBucketKey(); got != "" {
		t.Fatalf("empty workflow should return \"\", got %q", got)
	}
}

func TestProjectContextNarrows(t *testing.T) {
	p := Project{ID: 7, Name: "P", Slug: "p", RootPath: "/work"}
	got := p.Context()
	want := ProjectContext{ID: 7, Name: "P", Slug: "p", RootPath: "/work"}
	if got != want {
		t.Fatalf("Context() = %+v, want %+v", got, want)
	}
}

func TestInFlightBucketIDs(t *testing.T) {
	bucket := func(id int64, pos int) Bucket { return Bucket{ID: id, Position: pos} }

	tests := []struct {
		name     string
		buckets  []Bucket
		wantIDs  []int64
		wantOK   bool
	}{
		{
			name:    "zero-value workflow is unknown",
			buckets: nil,
			wantIDs: nil,
			wantOK:  false,
		},
		{
			name:    "single bucket: known, no in-flight stage",
			buckets: []Bucket{bucket(1, 1)},
			wantIDs: []int64{},
			wantOK:  true,
		},
		{
			name:    "two buckets: known, no middle",
			buckets: []Bucket{bucket(1, 1), bucket(4, 2)},
			wantIDs: []int64{},
			wantOK:  true,
		},
		{
			name:    "canonical four buckets: dev+review in-flight",
			buckets: []Bucket{bucket(1, 1), bucket(2, 2), bucket(3, 3), bucket(4, 4)},
			wantIDs: []int64{2, 3},
			wantOK:  true,
		},
		{
			name:    "unordered positions: min/max by position not slice order",
			buckets: []Bucket{bucket(3, 3), bucket(1, 1), bucket(4, 4), bucket(2, 2)},
			wantIDs: []int64{3, 2}, // slice order preserved; ids 3 and 2 are the middle positions
			wantOK:  true,
		},
		{
			name:    "tied extreme positions collapse the in-flight set",
			buckets: []Bucket{bucket(1, 1), bucket(2, 1), bucket(3, 1)},
			wantIDs: []int64{}, // min==max==1, so every bucket is an extreme -> none in-flight
			wantOK:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ids, ok := Workflow{Buckets: tc.buckets}.InFlightBucketIDs()
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if len(ids) != len(tc.wantIDs) {
				t.Fatalf("ids = %v, want %v", ids, tc.wantIDs)
			}
			for i := range ids {
				if ids[i] != tc.wantIDs[i] {
					t.Fatalf("ids = %v, want %v", ids, tc.wantIDs)
				}
			}
			// The zero-value case must return a nil slice (not empty) so the
			// caller's nil-check distinguishes unknown from known-empty.
			if !tc.wantOK && ids != nil {
				t.Fatalf("unknown workflow must return nil ids, got %v", ids)
			}
		})
	}
}
