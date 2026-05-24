package mcp

import (
	"testing"

	"omakiten/internal/domain"
)

// TestJSONRPCCodeForCategorisesDomainErrors pins the per-category
// JSON-RPC code mapping. Three buckets:
//
//   - -32602 (Invalid params) for caller-supplied input the server
//     can describe back.
//   - -32603 (Internal error) for server-side conditions the caller
//     cannot fix by re-asking.
//   - -32000 (server-defined) for business-rule rejections (not
//     found, conflict, guard violation, workflow refusal).
func TestJSONRPCCodeForCategorisesDomainErrors(t *testing.T) {
	cases := []struct {
		name string
		code domain.ErrorCode
		want int
	}{
		// Validation bucket — bad client input.
		{"validation_error", domain.ErrValidation, -32602},
		{"dependency_invalid", domain.ErrDependencyInvalid, -32602},
		{"tag_conflict", domain.ErrTagConflict, -32602},

		// Internal bucket — server / engine condition.
		{"config_invalid", domain.ErrConfigInvalid, -32603},
		{"config_too_large", domain.ErrConfigTooLarge, -32603},
		{"editor_failed", domain.ErrEditorFailed, -32603},
		{"editor_not_found", domain.ErrEditorNotFound, -32603},
		{"uninstall_failed", domain.ErrUninstallFailed, -32603},
		{"update_failed", domain.ErrUpdateFailed, -32603},

		// Business bucket — well-formed request the domain refused.
		{"task_not_found", domain.ErrTaskNotFound, -32000},
		{"project_not_found", domain.ErrProjectNotFound, -32000},
		{"project_ambiguous", domain.ErrProjectAmbiguous, -32000},
		{"bucket_not_found", domain.ErrBucketNotFound, -32000},
		{"law_not_found", domain.ErrLawNotFound, -32000},
		{"skill_not_found", domain.ErrSkillNotFound, -32000},
		{"persona_not_found", domain.ErrPersonaNotFound, -32000},
		{"skill_referenced", domain.ErrSkillReferenced, -32000},
		{"tag_not_found", domain.ErrTagNotFound, -32000},
		{"guard_violation", domain.ErrGuardViolation, -32000},
		{"error_not_found", domain.ErrErrorNotFound, -32000},
		{"solution_not_found", domain.ErrSolutionNotFound, -32000},
		{"plan_not_found", domain.ErrPlanNotFound, -32000},
		{"plan_slug_conflict", domain.ErrPlanSlugConflict, -32000},
		{"plan_wave_not_found", domain.ErrPlanWaveNotFound, -32000},
		{"workflow_invalid_transition", domain.ErrWorkflowInvalidTransition, -32000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := jsonRPCCodeFor(tc.code)
			if got != tc.want {
				t.Fatalf("jsonRPCCodeFor(%q) = %d, want %d", tc.code, got, tc.want)
			}
		})
	}
}

// TestErrorPayloadPreservesCodeInData asserts the envelope shape
// stayed stable while the wire-level code became category-specific:
// the per-code identifier still surfaces inside Data.code so agents
// that key on the exact domain code continue to work.
func TestErrorPayloadPreservesCodeInData(t *testing.T) {
	err := domain.NewError(domain.ErrTaskNotFound, "task not found", map[string]any{"id": int64(42)})
	payload := errorPayload(err)
	if payload.Code != -32000 {
		t.Fatalf("payload.Code = %d, want -32000 (business)", payload.Code)
	}
	data, ok := payload.Data.(map[string]any)
	if !ok {
		t.Fatalf("payload.Data not a map: %T", payload.Data)
	}
	if data["code"] != string(domain.ErrTaskNotFound) {
		t.Fatalf("Data.code = %v, want %q", data["code"], domain.ErrTaskNotFound)
	}
}
