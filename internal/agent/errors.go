package agent

import (
	"errors"

	"omakiten/internal/domain"
)

func FailureFromError(err error) Failure {
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		return Failure{Code: string(coded.Code), Message: coded.Message, Details: coded.Details, Guidance: guidanceForCode(coded.Code)}
	}

	return Failure{Code: "internal_error", Message: err.Error(), Guidance: Guidance{
		Message: "Omakiten hit an unexpected error while handling the agent intent.",
		Actions: []string{"Retry the intent once; if it repeats, inspect the Omakiten CLI output and logs."},
	}}
}

func guidanceForCode(code domain.ErrorCode) Guidance {
	switch code {
	case domain.ErrProjectNotFound:
		return Guidance{
			Message: "The current directory does not belong to a registered Omakiten project.",
			Actions: []string{"Run `okt init` from the project root.", "Or pass a registered project slug/id when invoking the agent intent."},
		}
	case domain.ErrProjectAmbiguous:
		return Guidance{
			Message: "More than one registered project matches the selector.",
			Actions: []string{"Pass an explicit project slug or project id."},
		}
	case domain.ErrTaskNotFound:
		return Guidance{
			Message: "The task was not found in the active project.",
			Actions: []string{"List active project tasks with `tasks.list`.", "Confirm the task id belongs to this project before continuing."},
		}
	case domain.ErrWorkflowInvalidTransition:
		return Guidance{
			Message: "The requested workflow move is not allowed by the active workflow.",
			Actions: []string{"Inspect allowed transitions with `workflow.show`.", "Move the task through the next allowed bucket first."},
		}
	case domain.ErrBucketNotFound:
		return Guidance{
			Message: "The requested workflow bucket does not exist in the active workflow.",
			Actions: []string{"Inspect bucket keys with `workflow.show`.", "Retry with one of the configured bucket keys."},
		}
	case domain.ErrDependencyInvalid:
		return Guidance{
			Message: "The requested dependency would violate Omakiten's dependency rules.",
			Actions: []string{"Inspect current dependencies with `dependencies.list`.", "Avoid self-dependencies, cycles, and cross-project task references."},
		}
	case domain.ErrValidation:
		return Guidance{
			Message: "The agent intent input is incomplete or invalid.",
			Actions: []string{"Check required fields and retry with a complete payload."},
		}
	case domain.ErrConfigInvalid:
		return Guidance{
			Message: "Omakiten configuration could not be loaded or materialized.",
			Actions: []string{"Run `okt config validate`.", "Fix the reported config issue before retrying the agent intent."},
		}
	default:
		return Guidance{
			Message: "Omakiten rejected the agent intent with a coded domain error.",
			Actions: []string{"Use the error code and details to adjust the next intent."},
		}
	}
}
