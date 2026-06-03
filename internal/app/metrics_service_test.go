package app

import (
	"context"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

func TestMetricsServiceSummaryAggregatesPerModel(t *testing.T) {
	store, project := appTestStore(t, appTestBundle(t))
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

	byModel := map[string]domain.AgentMetrics{}
	for _, m := range summary.ByModel {
		byModel[m.AgentModel] = m
	}

	opus, opusOK := byModel["claude-opus-4-7"]
	sonnet, sonnetOK := byModel["claude-sonnet-4-6"]
	if !opusOK || !sonnetOK {
		t.Fatalf("missing model in summary: opus_ok=%v sonnet_ok=%v", opusOK, sonnetOK)
	}

	if got := opus.Buckets[domain.MetricBucketErrorRecorded]; got != 1 {
		t.Fatalf("opus error_recorded = %d, want 1", got)
	}
	if got := sonnet.Buckets[domain.MetricBucketErrorRecorded]; got != 2 {
		t.Fatalf("sonnet error_recorded = %d, want 2", got)
	}

	// Opus searched once, recorded once with a session id, search came
	// before the record → ratio 100%. The like_rate denominator is the
	// SolutionAdded bucket; canonical formula is liked / added.
	if got := opus.Buckets[domain.MetricBucketErrorsResearched]; got != 1 {
		t.Fatalf("opus error_searched = %d, want 1", got)
	}
	if got := opus.Buckets[domain.MetricBucketSolutionLiked]; got != 1 {
		t.Fatalf("opus solution_liked = %d, want 1", got)
	}
	if got := opus.Buckets[domain.MetricBucketSolutionAdded]; got != 1 {
		t.Fatalf("opus solution_added = %d, want 1", got)
	}
	if opus.LikeRate != 1.0 {
		t.Fatalf("opus like_rate = %v, want 1.0", opus.LikeRate)
	}
	if got := int(opus.SearchBeforeRecordRatio * 100); got != 100 {
		t.Fatalf("opus search_before_record_ratio = %d%%, want 100%%", got)
	}

	// Sonnet recorded twice without searching → ratio 0%.
	if got := sonnet.Buckets[domain.MetricBucketErrorsResearched]; got != 0 {
		t.Fatalf("sonnet error_searched = %d, want 0", got)
	}
	if got := int(sonnet.SearchBeforeRecordRatio * 100); got != 0 {
		t.Fatalf("sonnet search_before_record_ratio = %d%%, want 0%%", got)
	}

	// Total reflects both models.
	if got := summary.Total.Buckets[domain.MetricBucketErrorRecorded]; got != 3 {
		t.Fatalf("total error_recorded = %d, want 3", got)
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

func TestMetricsServiceSummaryLikeRateFormula(t *testing.T) {
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	errService := NewErrorService(store, store.Snapshot())
	ctxAgent := activity.WithAgent(context.Background(), "mcp", "errors_record", "claude-haiku-4-7", "sess-haiku")

	// One error, two candidate solutions: one liked, one failed.
	// Expected: solution_added=2, solution_liked=1, solution_failed=1,
	// like_rate = 1/2 = 0.5 (canonical formula: liked / added).
	rec, err := errService.Record(ctxAgent, project.Context(), "race", "", nil)
	if err != nil {
		t.Fatalf("Record error = %v", err)
	}
	liked, err := errService.AddSolution(ctxAgent, project.Context(), rec.ID, "add mutex", "", nil)
	if err != nil {
		t.Fatalf("AddSolution(liked) error = %v", err)
	}
	failed, err := errService.AddSolution(ctxAgent, project.Context(), rec.ID, "retry loop", "", nil)
	if err != nil {
		t.Fatalf("AddSolution(failed) error = %v", err)
	}
	if _, err := errService.ConfirmSolution(ctxAgent, project.Context(), liked.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(true) error = %v", err)
	}
	if _, err := errService.ConfirmSolution(ctxAgent, project.Context(), failed.ID, false); err != nil {
		t.Fatalf("ConfirmSolution(false) error = %v", err)
	}

	summary, err := NewMetricsService(store).Summary(context.Background(), project.Context(), "30d", 0)
	if err != nil {
		t.Fatalf("Summary error = %v", err)
	}

	var haiku *domain.AgentMetrics
	for i := range summary.ByModel {
		if summary.ByModel[i].AgentModel == "claude-haiku-4-7" {
			haiku = &summary.ByModel[i]
			break
		}
	}
	if haiku == nil {
		t.Fatalf("haiku row missing from summary")
	}

	if got := haiku.Buckets[domain.MetricBucketSolutionAdded]; got != 2 {
		t.Fatalf("solution_added = %d, want 2", got)
	}
	if got := haiku.Buckets[domain.MetricBucketSolutionLiked]; got != 1 {
		t.Fatalf("solution_liked = %d, want 1", got)
	}
	if got := haiku.Buckets[domain.MetricBucketSolutionFailed]; got != 1 {
		t.Fatalf("solution_failed = %d, want 1", got)
	}
	if haiku.LikeRate != 0.5 {
		t.Fatalf("like_rate = %v, want 0.5 (liked/added = 1/2)", haiku.LikeRate)
	}
}

func TestMetricsServiceSummaryDefaultsPeriodTo30d(t *testing.T) {
	store, project := appTestStore(t, appTestBundle(t))
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
