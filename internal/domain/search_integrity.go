package domain

// SearchIndexRowRef identifies a source or index row without exposing its
// searchable content.
type SearchIndexRowRef struct {
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
	ProjectID  int64  `json:"project_id"`
}

// SearchIndexIssueSet carries a complete SQL count alongside a bounded detail
// sample. Truncated reports whether additional details were omitted.
type SearchIndexIssueSet[T any] struct {
	Count     int64 `json:"count"`
	Details   []T   `json:"details"`
	Truncated bool  `json:"truncated"`
}

// SearchIndexDuplicate identifies one logical key represented by more than
// one physical FTS row.
type SearchIndexDuplicate struct {
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
	IndexCount int64  `json:"index_count"`
}

// SearchIndexProjectMismatch reports routing metadata drift without exposing
// the indexed content.
type SearchIndexProjectMismatch struct {
	EntityType       string `json:"entity_type"`
	EntityID         int64  `json:"entity_id"`
	SourceProjectID  int64  `json:"source_project_id"`
	IndexedProjectID int64  `json:"indexed_project_id"`
}

// SearchIndexMalformed identifies an FTS row whose routing metadata uses an
// unexpected SQLite storage class. Values are coalesced to safe placeholders;
// searchable content and malformed raw metadata are never exposed.
type SearchIndexMalformed struct {
	EntityType        string `json:"entity_type"`
	EntityID          int64  `json:"entity_id"`
	ProjectID         int64  `json:"project_id"`
	EntityTypeStorage string `json:"entity_type_storage"`
	EntityIDStorage   string `json:"entity_id_storage"`
	ProjectIDStorage  string `json:"project_id_storage"`
}

// SearchIndexTypeReport compares one physical source type with its FTS rows.
// Unsupported types have SourceTotal zero and populate Unsupported.
type SearchIndexTypeReport struct {
	EntityType        string                                          `json:"entity_type"`
	SourceTotal       int64                                           `json:"source_total"`
	IndexTotal        int64                                           `json:"index_total"`
	Missing           SearchIndexIssueSet[SearchIndexRowRef]          `json:"missing"`
	Orphaned          SearchIndexIssueSet[SearchIndexRowRef]          `json:"orphaned"`
	Unsupported       SearchIndexIssueSet[SearchIndexRowRef]          `json:"unsupported"`
	Malformed         SearchIndexIssueSet[SearchIndexMalformed]       `json:"malformed"`
	Duplicates        SearchIndexIssueSet[SearchIndexDuplicate]       `json:"duplicates"`
	ContentMismatched SearchIndexIssueSet[SearchIndexRowRef]          `json:"content_mismatched"`
	ProjectMismatched SearchIndexIssueSet[SearchIndexProjectMismatch] `json:"project_mismatched"`
}

// SearchIndexTriggerReport describes drift from the canonical trigger set.
// Arbitrary unexpected names and SQL definitions are intentionally omitted.
type SearchIndexTriggerReport struct {
	ExpectedCount   int      `json:"expected_count"`
	ActualCount     int      `json:"actual_count"`
	UnexpectedCount int      `json:"unexpected_count"`
	Missing         []string `json:"missing"`
	Stale           []string `json:"stale"`
}

// SearchIndexFTSIntegrity is the result of FTS5's internal integrity-check
// command. Error is a fixed safe summary rather than the driver's raw error.
type SearchIndexFTSIntegrity struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SearchIndexIntegrityReport is the complete logical, trigger, and internal
// FTS5 health snapshot.
type SearchIndexIntegrityReport struct {
	Healthy     bool                     `json:"healthy"`
	SourceTotal int64                    `json:"source_total"`
	IndexTotal  int64                    `json:"index_total"`
	Types       []SearchIndexTypeReport  `json:"types"`
	Triggers    SearchIndexTriggerReport `json:"triggers"`
	FTS5        SearchIndexFTSIntegrity  `json:"fts5"`
}

func (r SearchIndexTypeReport) isHealthy() bool {
	return r.SourceTotal == r.IndexTotal &&
		r.Missing.Count == 0 &&
		r.Orphaned.Count == 0 &&
		r.Unsupported.Count == 0 &&
		r.Malformed.Count == 0 &&
		r.Duplicates.Count == 0 &&
		r.ContentMismatched.Count == 0 &&
		r.ProjectMismatched.Count == 0
}

func (r SearchIndexTypeReport) requiresBackupBeforeRepair() bool {
	return r.Orphaned.Count > 0 ||
		r.Unsupported.Count > 0 ||
		r.Malformed.Count > 0 ||
		r.Duplicates.Count > 0 ||
		r.ContentMismatched.Count > 0 ||
		r.ProjectMismatched.Count > 0
}

// IsHealthy derives report health from every logical, trigger, and FTS5 field.
func (r SearchIndexIntegrityReport) IsHealthy() bool {
	if r.SourceTotal != r.IndexTotal || !r.FTS5.OK || len(r.Triggers.Missing) > 0 || r.Triggers.UnexpectedCount > 0 || len(r.Triggers.Stale) > 0 {
		return false
	}
	for _, typeReport := range r.Types {
		if !typeReport.isHealthy() {
			return false
		}
	}
	return true
}

// RequiresBackupBeforeRepair reports whether repair would discard existing
// index evidence rather than only reconstruct missing canonical state.
func (r SearchIndexIntegrityReport) RequiresBackupBeforeRepair() bool {
	if r.Triggers.UnexpectedCount > 0 || len(r.Triggers.Stale) > 0 {
		return true
	}
	for _, typeReport := range r.Types {
		if typeReport.requiresBackupBeforeRepair() {
			return true
		}
	}
	return false
}

// SearchIndexReindexReport captures both sides of an atomic repair.
type SearchIndexReindexReport struct {
	Before            SearchIndexIntegrityReport `json:"before"`
	After             SearchIndexIntegrityReport `json:"after"`
	BackupRecommended bool                       `json:"backup_recommended"`
}
