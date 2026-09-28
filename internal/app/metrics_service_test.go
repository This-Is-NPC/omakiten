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
	ctxOpus := activity.WithAgent(context.Background(), "cli", "errors_record", "claude-opus-4-7", "sess-opus")
	ctxSonnet := activity.WithAgent(context.Background(), "cli", "errors_record", "claude-sonnet-4-6", "sess-sonnet")

	recordMetricsScenario(t, errService, searchService, project.Context(), ctxOpus, ctxSonnet)

	metrics := NewMetricsService(store)
	summary, err := metrics.Summary(context.Background(), project.Context(), "30d", 0)
	if err != nil {
		t.Fatalf("Summary error = %v", err)
	}

	assertMetricsSummary(t, summary)
}

func recordMetricsScenario(t *testing.T, errorsService *ErrorService, searchService *SearchService, project domain.ProjectContext, opus, sonnet context.Context) {
	if _, err := searchService.Search(opus, project, "fk", []string{"error"}); err != nil {
		t.Fatalf("Search error = %v", err)
	}
	rec, err := errorsService.Record(opus, project, "FK violation", "", nil)
	if err != nil {
		t.Fatalf("Record error = %v", err)
	}
	sol, err := errorsService.AddSolution(opus, project, rec.ID, "drop fk", "", nil)
	if err != nil {
		t.Fatalf("AddSolution error = %v", err)
	}
	if _, err := errorsService.ConfirmSolution(opus, project, sol.ID, true); err != nil {
		t.Fatalf("ConfirmSolution error = %v", err)
	}
	for _, description := range []string{"panic", "deadlock"} {
		if _, err := errorsService.Record(sonnet, project, description, "", nil); err != nil {
			t.Fatalf("Record(%s) error = %v", description, err)
		}
	}
}

func assertMetricsSummary(t *testing.T, summary domain.MetricsSummary) {
	byModel := map[string]domain.AgentMetrics{}
	for _, metric := range summary.ByModel {
		byModel[metric.AgentModel] = metric
	}
	opus, opusOK := byModel["claude-opus-4-7"]
	sonnet, sonnetOK := byModel["claude-sonnet-4-6"]
	if !opusOK || !sonnetOK {
		t.Fatalf("missing model in summary: opus_ok=%v sonnet_ok=%v", opusOK, sonnetOK)
	}
	assertMetricBuckets(t, opus, sonnet)
	if summary.Total.Buckets[domain.MetricBucketErrorRecorded] != 3 || summary.Total.SessionCorrelatedSample != 3 {
		t.Fatalf("total metrics = %+v, want recorded=3 and sample=3", summary.Total)
	}
	if got := int(summary.Total.SearchBeforeRecordRatio*100 + 0.5); got != 33 {
		t.Fatalf("total search_before_record_ratio = %d%%, want 33%%", got)
	}
}

func assertMetricBuckets(t *testing.T, opus, sonnet domain.AgentMetrics) {
	if opus.Buckets[domain.MetricBucketErrorRecorded] != 1 || sonnet.Buckets[domain.MetricBucketErrorRecorded] != 2 {
		t.Fatalf("recorded buckets: opus=%d sonnet=%d, want 1 and 2", opus.Buckets[domain.MetricBucketErrorRecorded], sonnet.Buckets[domain.MetricBucketErrorRecorded])
	}
	if opus.Buckets[domain.MetricBucketErrorsResearched] != 1 || opus.Buckets[domain.MetricBucketSolutionLiked] != 1 || opus.Buckets[domain.MetricBucketSolutionAdded] != 1 || opus.LikeRate != 1.0 || int(opus.SearchBeforeRecordRatio*100) != 100 {
		t.Fatalf("opus metrics = %+v, want one search/like/add and 100%% ratios", opus)
	}
	if sonnet.Buckets[domain.MetricBucketErrorsResearched] != 0 || int(sonnet.SearchBeforeRecordRatio*100) != 0 {
		t.Fatalf("sonnet metrics = %+v, want no search and 0%% ratio", sonnet)
	}
}

func TestMetricsServiceSummaryLikeRateFormula(t *testing.T) {
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	errService := NewErrorService(store, store.Snapshot())
	ctxAgent := activity.WithAgent(context.Background(), "cli", "errors_record", "claude-haiku-4-7", "sess-haiku")

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
