package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// InsightOperations searches a project and summarises its activity.
type InsightOperations interface {
	Search(ctx context.Context, input contract.SearchInput) (contract.SearchResponse, error)
	ListLogs(ctx context.Context, input contract.ListLogsInput) (contract.ListLogsResponse, error)
	InsightsSummary(ctx context.Context, input contract.InsightsSummaryInput) (contract.InsightsSummaryResponse, error)
	MetricsSummary(ctx context.Context, input contract.MetricsSummaryInput) (contract.MetricsSummaryResponse, error)
}

func (s *Server) insightRoutes() []route {
	return []route{
		query("search", http.MethodGet, projectPath+"/search", "search", "Full-text search within a project.", []param{
			projectParam,
			queryParam("q", "Search query.", stringSchema),
			queryParam("type", "Entity type filter; repeatable or comma-separated.", listSchema),
		}, s.search),
		query("listLogs", http.MethodGet, projectPath+"/logs", "logs.list", "Event log of a project.", []param{
			projectParam,
			queryParam("category", "Event category; repeatable or comma-separated.", listSchema),
			queryParam("since", "Window such as `24h` or `7d`.", stringSchema),
			queryParam("limit", "Row cap.", intSchema),
			queryParam("order", "`asc` or `desc` (default).", stringSchema),
		}, s.listLogs),
		query("getInsights", http.MethodGet, projectPath+"/insights", "insights.summary", "Today's insights for a project.", []param{
			projectParam,
			queryParam("stuck_days", "Days without movement that mark a task stuck.", intSchema),
		}, s.insights),
		query("getMetrics", http.MethodGet, projectPath+"/metrics", "metrics.summary", "Task and event metrics for a project.", []param{
			projectParam,
			queryParam("period", "Window such as `7d`.", stringSchema),
		}, s.metrics),
	}
}

func (s *Server) search(r *http.Request) (contract.SearchResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.SearchResponse{}, err
	}
	return ops.Search(r.Context(), contract.SearchInput{ProjectSelector: selector, Query: r.URL.Query().Get("q"), EntityTypes: listValues(r, "type")})
}

func (s *Server) listLogs(r *http.Request) (contract.ListLogsResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ListLogsResponse{}, err
	}
	limit, err := optionalInt(r, "limit")
	if err != nil {
		return contract.ListLogsResponse{}, err
	}
	return ops.ListLogs(r.Context(), contract.ListLogsInput{
		ProjectSelector: selector,
		Categories:      listValues(r, "category"),
		Since:           r.URL.Query().Get("since"),
		Limit:           limit,
		Order:           r.URL.Query().Get("order"),
	})
}

func (s *Server) insights(r *http.Request) (contract.InsightsSummaryResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.InsightsSummaryResponse{}, err
	}
	stuckDays, err := optionalInt(r, "stuck_days")
	if err != nil {
		return contract.InsightsSummaryResponse{}, err
	}
	return ops.InsightsSummary(r.Context(), contract.InsightsSummaryInput{ProjectSelector: selector, StuckDays: stuckDays})
}

func (s *Server) metrics(r *http.Request) (contract.MetricsSummaryResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.MetricsSummaryResponse{}, err
	}
	return ops.MetricsSummary(r.Context(), contract.MetricsSummaryInput{ProjectSelector: selector, Period: r.URL.Query().Get("period"), ProjectID: selector.ProjectID})
}
