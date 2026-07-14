package domain

import "testing"

func TestSearchIndexIntegrityReportIsHealthyRejectsEveryDriftField(t *testing.T) {
	t.Parallel()

	healthyReport := func() SearchIndexIntegrityReport {
		return SearchIndexIntegrityReport{
			SourceTotal: 1,
			IndexTotal:  1,
			Types: []SearchIndexTypeReport{{
				EntityType:  "task",
				SourceTotal: 1,
				IndexTotal:  1,
			}},
			FTS5: SearchIndexFTSIntegrity{OK: true},
		}
	}
	cases := map[string]func(*SearchIndexIntegrityReport){
		"report totals":      func(report *SearchIndexIntegrityReport) { report.IndexTotal++ },
		"FTS5":               func(report *SearchIndexIntegrityReport) { report.FTS5.OK = false },
		"missing trigger":    func(report *SearchIndexIntegrityReport) { report.Triggers.Missing = []string{"search_index_tasks_ai"} },
		"unexpected trigger": func(report *SearchIndexIntegrityReport) { report.Triggers.UnexpectedCount = 1 },
		"stale trigger":      func(report *SearchIndexIntegrityReport) { report.Triggers.Stale = []string{"search_index_tasks_ai"} },
		"type totals":        func(report *SearchIndexIntegrityReport) { report.Types[0].IndexTotal++ },
		"missing":            func(report *SearchIndexIntegrityReport) { report.Types[0].Missing.Count = 1 },
		"orphaned":           func(report *SearchIndexIntegrityReport) { report.Types[0].Orphaned.Count = 1 },
		"unsupported":        func(report *SearchIndexIntegrityReport) { report.Types[0].Unsupported.Count = 1 },
		"malformed":          func(report *SearchIndexIntegrityReport) { report.Types[0].Malformed.Count = 1 },
		"duplicates":         func(report *SearchIndexIntegrityReport) { report.Types[0].Duplicates.Count = 1 },
		"content mismatched": func(report *SearchIndexIntegrityReport) { report.Types[0].ContentMismatched.Count = 1 },
		"project mismatched": func(report *SearchIndexIntegrityReport) { report.Types[0].ProjectMismatched.Count = 1 },
	}
	if !healthyReport().IsHealthy() {
		t.Fatal("baseline report is unhealthy")
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report := healthyReport()
			mutate(&report)
			if report.IsHealthy() {
				t.Fatal("drifted report is healthy")
			}
		})
	}
}

func TestSearchIndexIntegrityReportRequiresBackupBeforeRepairForEachDestructiveField(t *testing.T) {
	t.Parallel()

	destructive := map[string]func(*SearchIndexIntegrityReport){
		"unexpected trigger": func(report *SearchIndexIntegrityReport) { report.Triggers.UnexpectedCount = 1 },
		"stale trigger":      func(report *SearchIndexIntegrityReport) { report.Triggers.Stale = []string{"search_index_tasks_ai"} },
		"orphaned":           func(report *SearchIndexIntegrityReport) { report.Types[0].Orphaned.Count = 1 },
		"unsupported":        func(report *SearchIndexIntegrityReport) { report.Types[0].Unsupported.Count = 1 },
		"malformed":          func(report *SearchIndexIntegrityReport) { report.Types[0].Malformed.Count = 1 },
		"duplicates":         func(report *SearchIndexIntegrityReport) { report.Types[0].Duplicates.Count = 1 },
		"content mismatched": func(report *SearchIndexIntegrityReport) { report.Types[0].ContentMismatched.Count = 1 },
		"project mismatched": func(report *SearchIndexIntegrityReport) { report.Types[0].ProjectMismatched.Count = 1 },
	}
	for name, mutate := range destructive {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report := SearchIndexIntegrityReport{Types: []SearchIndexTypeReport{{EntityType: "task"}}}
			mutate(&report)
			if !report.RequiresBackupBeforeRepair() {
				t.Fatal("RequiresBackupBeforeRepair() = false, want true")
			}
		})
	}
}

func TestSearchIndexIntegrityReportDoesNotRequireBackupForReconstructiveDrift(t *testing.T) {
	t.Parallel()

	reports := map[string]SearchIndexIntegrityReport{
		"missing row": {
			Types: []SearchIndexTypeReport{{EntityType: "task", Missing: SearchIndexIssueSet[SearchIndexRowRef]{Count: 1}}},
		},
		"missing trigger": {
			Triggers: SearchIndexTriggerReport{Missing: []string{"search_index_tasks_ai"}},
		},
	}
	for name, report := range reports {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if report.RequiresBackupBeforeRepair() {
				t.Fatal("RequiresBackupBeforeRepair() = true, want false")
			}
		})
	}
}
