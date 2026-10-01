package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"search", http.MethodGet, "/api/v1/projects/alpha/search?q=login+bug&type=task,comment", "", "Search", contract.SearchInput{ProjectSelector: alpha, Query: "login bug", EntityTypes: []string{"task", "comment"}}},
	{"listLogs", http.MethodGet, "/api/v1/projects/alpha/logs?category=task&category=plan&since=7d&limit=10&order=asc", "", "ListLogs", contract.ListLogsInput{
		ProjectSelector: alpha, Categories: []string{"task", "plan"}, Since: "7d", Limit: 10, Order: "asc",
	}},
	{"getInsights", http.MethodGet, "/api/v1/projects/alpha/insights?stuck_days=3", "", "InsightsSummary", contract.InsightsSummaryInput{ProjectSelector: alpha, StuckDays: 3}},
	{"getMetrics", http.MethodGet, "/api/v1/projects/alpha/metrics?period=7d", "", "MetricsSummary", contract.MetricsSummaryInput{ProjectSelector: alpha, Period: "7d", ProjectID: 7}},
})

func (f *fakeOps) Search(_ context.Context, in contract.SearchInput) (contract.SearchResponse, error) {
	f.record("Search", in)
	return contract.SearchResponse{}, nil
}

func (f *fakeOps) ListLogs(_ context.Context, in contract.ListLogsInput) (contract.ListLogsResponse, error) {
	if f.listLogs != nil {
		return f.listLogs(in)
	}
	f.record("ListLogs", in)
	return contract.ListLogsResponse{}, nil
}

func (f *fakeOps) InsightsSummary(_ context.Context, in contract.InsightsSummaryInput) (contract.InsightsSummaryResponse, error) {
	f.record("InsightsSummary", in)
	return contract.InsightsSummaryResponse{}, nil
}

func (f *fakeOps) MetricsSummary(_ context.Context, in contract.MetricsSummaryInput) (contract.MetricsSummaryResponse, error) {
	f.record("MetricsSummary", in)
	return contract.MetricsSummaryResponse{}, nil
}
