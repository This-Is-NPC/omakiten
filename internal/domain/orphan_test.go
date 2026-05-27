package domain

import "testing"

func TestOrphanPayloadJSON(t *testing.T) {
	cases := map[string]struct {
		json func() (string, error)
		want string
	}{
		"task bucket orphaned": {
			json: func() (string, error) {
				parentID := int64(7)
				return TaskBucketOrphanedPayload{TaskID: 9, ParentID: &parentID, Depth: 2, OldBucket: "old", FromKit: "root", ToKit: "sub", ResolvedKit: "sub", Reason: "bucket_missing_in_resolved_kit"}.JSON()
			},
			want: `{"task_id":9,"parent_id":7,"depth":2,"old_bucket":"old","from_kit":"root","to_kit":"sub","resolved_kit":"sub","reason":"bucket_missing_in_resolved_kit"}`,
		},
		"task migrated": {
			json: func() (string, error) {
				return TaskMigratedPayload{From: "backlog", To: "review", Reason: "workflow_swap"}.JSON()
			},
			want: `{"from":"backlog","to":"review","reason":"workflow_swap"}`,
		},
		"subtask kit notice": {
			json: func() (string, error) {
				return SubtaskKitNoticePayload{I18nKey: "notice", FromKit: "root", ToKit: "sub"}.JSON()
			},
			want: `{"i18n_key":"notice","from_kit":"root","to_kit":"sub"}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.json()
			if err != nil {
				t.Fatalf("JSON() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("JSON() = %s, want %s", got, tc.want)
			}
		})
	}
}
