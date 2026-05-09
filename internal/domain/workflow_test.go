package domain

import "testing"

func boolPtr(b bool) *bool { return &b }

func TestResolveTaskPermission(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		bucket    Bucket
		defaults  *WorkflowDefaults
		wantEdit  bool
		wantDel   bool
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
		bucket    Bucket
		defaults  *WorkflowDefaults
		wantEdit  bool
		wantDel   bool
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

func TestResolveBoolDirectly(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   []*bool
		want bool
	}{
		"all nil — implicit true":     {in: []*bool{nil, nil}, want: true},
		"first non-nil wins (false)":  {in: []*bool{boolPtr(false), boolPtr(true)}, want: false},
		"first non-nil wins (true)":   {in: []*bool{nil, boolPtr(true), boolPtr(false)}, want: true},
		"single nil — implicit true":  {in: []*bool{nil}, want: true},
		"empty — implicit true":       {in: nil, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := resolveBool(tc.in...); got != tc.want {
				t.Fatalf("resolveBool(%v) = %v, want %v", tc.in, got, tc.want)
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
