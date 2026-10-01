package httpapi

import (
	"slices"
	"strconv"
	"strings"

	"net/http"
	"omakiten/internal/contract"
)

const (
	projectsPath = "/api/v1/projects"
	projectPath  = projectsPath + "/{project}"
	taskPath     = projectPath + "/tasks/{task}"
	planPath     = projectPath + "/plans/{plan}"
)

var (
	projectParam = pathParam("project", "Project slug.", stringSchema)
	taskParam    = pathParam("task", "Task id.", idSchema)
	// confirmedParam gates a destructive route; without it the answer asks
	// for confirmation and changes nothing.
	confirmedParam = queryParam("confirmed", "`true` performs the change; otherwise the answer asks for confirmation.", boolSchema)
	planParam      = pathParam("plan", "Plan slug.", stringSchema)
)

// HealthResponse reports daemon liveness.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// CatalogResponse carries every catalog entry for the GUI language.
type CatalogResponse struct {
	Language string            `json:"language"`
	Entries  map[string]string `json:"entries"`
}

func (s *Server) routes() []route {
	return slices.Concat(
		[]route{
			s.healthRoute(),
			query("getCatalog", http.MethodGet, "/api/v1/catalog", "", "Text for the GUI language preference.", nil, s.catalog),
		},
		s.projectRoutes(),
		s.taskRoutes(),
		s.commentRoutes(),
		s.dependencyRoutes(),
		s.planRoutes(),
		s.workDocumentRoutes(),
		s.insightRoutes(),
		s.maintenanceRoutes(),
		s.recordRoutes(),
		s.tagRoutes(),
		s.catalogRoutes(),
		[]route{
			query("getKnowledge", http.MethodGet, projectPath+"/knowledge", "", "File-backed knowledge of a project and its related projects.", []param{projectParam}, s.knowledge),
			query("getStudio", http.MethodGet, projectPath+"/studio", "", "Workflow guards, agent wiring, hooks, and effective settings.", []param{projectParam}, s.studio),
			s.eventsRoute(),
		},
	)
}

func (s *Server) healthRoute() route {
	r := query("getHealth", http.MethodGet, "/health", "", "Daemon liveness; needs no token.", nil, func(*http.Request) (HealthResponse, error) {
		return HealthResponse{Status: "ok", Version: s.opts.Version}, nil
	})
	r.public = true
	return r
}

func (s *Server) catalog(*http.Request) (CatalogResponse, error) {
	catalog := s.opts.Runtimes.Catalog()
	return CatalogResponse{Language: catalog.Code(), Entries: catalog.Entries()}, nil
}

func (s *Server) project(r *http.Request) (Operations, contract.ProjectSelector, error) {
	return s.opts.Runtimes.Project(r.Context(), r.PathValue("project"))
}

func (s *Server) task(r *http.Request) (Operations, contract.ProjectSelector, int64, error) {
	taskID, err := parseID(r.PathValue("task"), "task")
	if err != nil {
		return nil, contract.ProjectSelector{}, 0, err
	}
	ops, selector, err := s.project(r)
	return ops, selector, taskID, err
}

func parseID(raw, name string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, invalidParameter(name, raw)
	}
	return id, nil
}

func optionalInt(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalidParameter(name, raw)
	}
	return value, nil
}

// optionalBool returns nil when the query value is absent.
func optionalBool(r *http.Request, name string) (*bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, invalidParameter(name, raw)
	}
	return &value, nil
}

// confirmed reads the confirmedParam flag; absent means false.
func confirmed(r *http.Request) (bool, error) {
	value, err := optionalBool(r, "confirmed")
	return value != nil && *value, err
}

// listValues accepts repeated and comma-separated query values.
func listValues(r *http.Request, name string) []string {
	var out []string
	for _, raw := range r.URL.Query()[name] {
		for value := range strings.SplitSeq(raw, ",") {
			if value = strings.TrimSpace(value); value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}
