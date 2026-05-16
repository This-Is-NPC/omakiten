package app

import (
	"context"
	"testing"

	"omakiten/internal/activity"
)

func TestMetricsServiceSummaryAggregatesPerModel(t *testing.T) {
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	errService := NewErrorService(store, store.Snapshot())
	searchService := NewSearchService(store, store)

	// Two distinct models, distinct sessions.
	ctxOpus := activity.WithAgent(context.Background(), "mcp", "errors_record", "claude-opus-4-7", "sess-opus")
	ctxSonnet := activity.WithAgent(context.Background(), "mcp", "errors_record", "claude-sonnet-4-6", "sess-sonnet")

	// Opus: search first (cross-entity, error scope), then record.
	// Add a solution and like it.
	if _, err := searchService.Search(ctxOpus, project.Context(), "fk", []string{"error"}); err != nil {
		t.Fatalf("Search error = %v", err)
	}
	rec, err := errService.Record(ctxOpus, project.Context(), "FK violation", "", nil)
	if err != nil {
		t.Fatalf("Record error = %v", err)
	}
	sol, err := errService.AddSolution(ctxOpus, project.Context(), rec.ID, "drop fk", "", nil)
	if err != nil {
		t.Fatalf("AddSolution error = %v", err)
	}
	if _, err := errService.ConfirmSolution(ctxOpus, project.Context(), sol.ID, true); err != nil {
		t.Fatalf("ConfirmSolution error = %v", err)
	}

	// Sonnet: records without searching.
	if _, err := errService.Record(ctxSonnet, project.Context(), "panic", "", nil); err != nil {
		t.Fatalf("Record error = %v", err)
	}
	if _, err := errService.Record(ctxSonnet, project.Context(), "deadlock", "", nil); err != nil {
		t.Fatalf("Record error = %v", err)
	}

	metrics := NewMetricsService(store)
	summary, err := metrics.Summary(context.Background(), project.Context(), "30d", 0)
	if err != nil {
		t.Fatalf("Summary error = %v", err)
	}

	byModel := map[string]int{}
	for _, m := range summary.ByModel {
		byModel[m.AgentModel] = m.ErrorsRecorded
	}
	if byModel["claude-opus-4-7"] != 1 {
		t.Fatalf("opus errors_recorded = %d, want 1", byModel["claude-opus-4-7"])
	}
	if byModel["claude-sonnet-4-6"] != 2 {
		t.Fatalf("sonnet errors_recorded = %d, want 2", byModel["claude-sonnet-4-6"])
	}

	var opus, sonnet *struct {
		Searched, Liked, SearchRatio, Sample, Recorded int
		LikeRate                                       float64
	}
	for i := range summary.ByModel {
		m := summary.ByModel[i]
		if m.AgentModel == "claude-opus-4-7" {
			opus = &struct {
				Searched, Liked, SearchRatio, Sample, Recorded int
				LikeRate                                       float64
			}{
				Searched:    m.ErrorsSearched,
				Liked:       m.SolutionsLiked,
				SearchRatio: int(m.SearchBeforeRecordRatio * 100),
				Sample:      m.SessionCorrelatedSample,
				Recorded:    m.ErrorsRecorded,
				LikeRate:    m.LikeRate,
			}
		}
		if m.AgentModel == "claude-sonnet-4-6" {
			sonnet = &struct {
				Searched, Liked, SearchRatio, Sample, Recorded int
				LikeRate                                       float64
			}{
				Searched:    m.ErrorsSearched,
				Liked:       m.SolutionsLiked,
				SearchRatio: int(m.SearchBeforeRecordRatio * 100),
				Sample:      m.SessionCorrelatedSample,
				Recorded:    m.ErrorsRecorded,
				LikeRate:    m.LikeRate,
			}
		}
	}

	if opus == nil || sonnet == nil {
		t.Fatalf("missing model in summary: opus=%v sonnet=%v", opus, sonnet)
	}

	// Opus searched once, recorded once with a session id, search came
	// before the record → ratio 100%.
	if opus.Searched != 1 {
		t.Fatalf("opus errors_searched = %d, want 1", opus.Searched)
	}
	if opus.Liked != 1 {
		t.Fatalf("opus solutions_liked = %d, want 1", opus.Liked)
	}
	if opus.LikeRate != 1.0 {
		t.Fatalf("opus like_rate = %v, want 1.0", opus.LikeRate)
	}
	if opus.SearchRatio != 100 {
		t.Fatalf("opus search_before_record_ratio = %d%%, want 100%%", opus.SearchRatio)
	}

	// Sonnet recorded twice without searching → ratio 0%.
	if sonnet.Searched != 0 {
		t.Fatalf("sonnet errors_searched = %d, want 0", sonnet.Searched)
	}
	if sonnet.SearchRatio != 0 {
		t.Fatalf("sonnet search_before_record_ratio = %d%%, want 0%%", sonnet.SearchRatio)
	}

	// Total reflects both models.
	if summary.Total.ErrorsRecorded != 3 {
		t.Fatalf("total errors_recorded = %d, want 3", summary.Total.ErrorsRecorded)
	}

	// Total.SearchBeforeRecordRatio is reconstructed from absolute counts so
	// it weights samples correctly: opus has 1 search-before-record over
	// 1 session sample; sonnet has 0 over 2; combined sample is 3 → ratio 1/3.
	if summary.Total.SessionCorrelatedSample != 3 {
		t.Fatalf("total session_correlated_sample = %d, want 3", summary.Total.SessionCorrelatedSample)
	}
	if got := int(summary.Total.SearchBeforeRecordRatio*100 + 0.5); got != 33 {
		t.Fatalf("total search_before_record_ratio = %d%%, want 33%%", got)
	}
}

func TestMetricsServiceSummaryDefaultsPeriodTo30d(t *testing.T) {
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	metrics := NewMetricsService(store)
	summary, err := metrics.Summary(context.Background(), project.Context(), "", 0)
	if err != nil {
		t.Fatalf("Summary error = %v", err)
	}
	if summary.Period != "30d" {
		t.Fatalf("Summary().Period = %q, want 30d", summary.Period)
	}

	summary, err = metrics.Summary(context.Background(), project.Context(), "lifetime", 0)
	if err != nil {
		t.Fatalf("Summary error = %v", err)
	}
	if summary.Period != "30d" {
		t.Fatalf("invalid period fallback = %q, want 30d", summary.Period)
	}
}
