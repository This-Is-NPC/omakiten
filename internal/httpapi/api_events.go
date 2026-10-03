package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

// EventOperations takes the events outside callers emit.
type EventOperations interface {
	EmitEvent(ctx context.Context, input contract.EmitEventInput) (contract.EmitEventResponse, error)
}

// EventBody is the emitEvent request body: the declared name, without its
// external. prefix, and the event's fields.
type EventBody struct {
	Name    string            `json:"name"`
	Payload map[string]string `json:"payload,omitempty"`
}

func (s *Server) eventRoutes() []route {
	return []route{
		command("emitEvent", http.MethodPost, projectPath+"/events", "event.emit", "Emit an external event the project's kit declares; the hooks on it run.", []param{projectParam}, s.emitEvent),
	}
}

func (s *Server) emitEvent(r *http.Request, body EventBody) (contract.EmitEventResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.EmitEventResponse{}, err
	}
	return ops.EmitEvent(r.Context(), contract.EmitEventInput{ProjectSelector: selector, Name: body.Name, Payload: body.Payload})
}
