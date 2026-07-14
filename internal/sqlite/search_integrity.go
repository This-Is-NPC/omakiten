package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	modernsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"omakiten/internal/domain"
)

const canonicalSearchSourceCTE = `
WITH canonical(content, entity_type, entity_id, project_id) AS (
  SELECT COALESCE(title, '') || ' ' || COALESCE(description, ''), 'task', id, project_id FROM tasks
  UNION ALL
  SELECT COALESCE(body, '') || ' ' || COALESCE(title, ''), 'comment', id, COALESCE(project_id, 0)
    FROM events WHERE event_type = 'comment'
  UNION ALL
  SELECT COALESCE(description, '') || ' ' || COALESCE(context, ''), 'error', id, COALESCE(project_id, 0) FROM errors
  UNION ALL
  SELECT COALESCE(s.description, '') || ' ' || COALESCE(s.steps, ''), 'solution', s.id,
         COALESCE(e.project_id, 0)
    FROM solutions s LEFT JOIN errors e ON e.id = s.error_id
  UNION ALL
  SELECT COALESCE(name, '') || ' ' || COALESCE(goal_body, ''), 'plan', id, project_id FROM plans
)
`

const searchIndexIssueRowsSQL = `
SELECT 'missing' AS kind, c.entity_type, c.entity_id, c.project_id, 0 AS indexed_project_id,
       '' AS entity_type_storage, '' AS entity_id_storage, '' AS project_id_storage
  FROM search_check_canonical c
  LEFT JOIN search_check_index i
    ON i.metadata_valid = 1 AND i.supported = 1
   AND i.entity_type = c.entity_type AND i.entity_id = c.entity_id
 WHERE i.index_rowid IS NULL
UNION ALL
SELECT 'orphaned', i.entity_type, i.entity_id, i.project_id, 0, '', '', ''
  FROM search_check_index i
  LEFT JOIN search_check_canonical c
    ON c.entity_type = i.entity_type AND c.entity_id = i.entity_id
 WHERE i.metadata_valid = 1 AND i.supported = 1 AND c.entity_id IS NULL
UNION ALL
SELECT 'unsupported', i.entity_type, i.entity_id, i.project_id, 0, '', '', ''
  FROM search_check_index i
 WHERE i.metadata_valid = 1 AND i.supported = 0
UNION ALL
SELECT 'malformed', i.entity_type, i.entity_id, i.project_id, 0,
       i.entity_type_storage, i.entity_id_storage, i.project_id_storage
  FROM search_check_index i
 WHERE i.metadata_valid = 0
UNION ALL
SELECT 'duplicates', i.entity_type, i.entity_id, 0, COUNT(*), '', '', ''
  FROM search_check_index i
 WHERE i.metadata_valid = 1
 GROUP BY i.entity_type, i.entity_id
HAVING COUNT(*) > 1
UNION ALL
SELECT 'content', c.entity_type, c.entity_id, c.project_id, 0, '', '', ''
  FROM search_check_canonical c
  JOIN search_check_index i
    ON i.metadata_valid = 1 AND i.supported = 1
   AND i.entity_type = c.entity_type AND i.entity_id = c.entity_id
 WHERE i.content IS NOT c.content
 GROUP BY c.entity_type, c.entity_id, c.project_id
UNION ALL
SELECT 'project', c.entity_type, c.entity_id, c.project_id, MIN(i.project_id), '', '', ''
  FROM search_check_canonical c
  JOIN search_check_index i
    ON i.metadata_valid = 1 AND i.supported = 1
   AND i.entity_type = c.entity_type AND i.entity_id = c.entity_id
 WHERE i.project_id != c.project_id
 GROUP BY c.entity_type, c.entity_id, c.project_id
`

var canonicalSearchTriggers = map[string]string{
	"search_index_tasks_ai": `CREATE TRIGGER IF NOT EXISTS search_index_tasks_ai AFTER INSERT ON tasks BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.description, ''), 'task', NEW.id, NEW.project_id);
END`,
	"search_index_tasks_au": `CREATE TRIGGER IF NOT EXISTS search_index_tasks_au AFTER UPDATE ON tasks BEGIN
  DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.description, ''), 'task', NEW.id, NEW.project_id);
END`,
	"search_index_tasks_ad": `CREATE TRIGGER IF NOT EXISTS search_index_tasks_ad AFTER DELETE ON tasks BEGIN
  DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = OLD.id;
END`,
	"search_index_comments_ai": `CREATE TRIGGER IF NOT EXISTS search_index_comments_ai AFTER INSERT ON events
WHEN NEW.event_type = 'comment' BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.body, '') || ' ' || COALESCE(NEW.title, ''), 'comment', NEW.id, COALESCE(NEW.project_id, 0));
END`,
	"search_index_comments_au": `CREATE TRIGGER IF NOT EXISTS search_index_comments_au AFTER UPDATE ON events
WHEN NEW.event_type = 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.body, '') || ' ' || COALESCE(NEW.title, ''), 'comment', NEW.id, COALESCE(NEW.project_id, 0));
END`,
	"search_index_comments_ad": `CREATE TRIGGER IF NOT EXISTS search_index_comments_ad AFTER DELETE ON events
WHEN OLD.event_type = 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END`,
	"search_index_comments_au_demote": `CREATE TRIGGER IF NOT EXISTS search_index_comments_au_demote AFTER UPDATE ON events
WHEN OLD.event_type = 'comment' AND NEW.event_type != 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END`,
	"search_index_errors_ai": `CREATE TRIGGER IF NOT EXISTS search_index_errors_ai AFTER INSERT ON errors BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.context, ''), 'error', NEW.id, COALESCE(NEW.project_id, 0));
END`,
	"search_index_errors_au": `CREATE TRIGGER IF NOT EXISTS search_index_errors_au AFTER UPDATE ON errors BEGIN
  DELETE FROM search_index WHERE entity_type = 'error' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.context, ''), 'error', NEW.id, COALESCE(NEW.project_id, 0));
END`,
	"search_index_errors_ad": `CREATE TRIGGER IF NOT EXISTS search_index_errors_ad AFTER DELETE ON errors BEGIN
  DELETE FROM search_index WHERE entity_type = 'error' AND entity_id = OLD.id;
END`,
	"search_index_solutions_ai": `CREATE TRIGGER IF NOT EXISTS search_index_solutions_ai AFTER INSERT ON solutions BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.steps, ''), 'solution', NEW.id,
          COALESCE((SELECT project_id FROM errors WHERE id = NEW.error_id), 0));
END`,
	"search_index_solutions_au": `CREATE TRIGGER IF NOT EXISTS search_index_solutions_au AFTER UPDATE ON solutions BEGIN
  DELETE FROM search_index WHERE entity_type = 'solution' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.steps, ''), 'solution', NEW.id,
          COALESCE((SELECT project_id FROM errors WHERE id = NEW.error_id), 0));
END`,
	"search_index_solutions_ad": `CREATE TRIGGER IF NOT EXISTS search_index_solutions_ad AFTER DELETE ON solutions BEGIN
  DELETE FROM search_index WHERE entity_type = 'solution' AND entity_id = OLD.id;
END`,
	"search_index_plans_ai": `CREATE TRIGGER IF NOT EXISTS search_index_plans_ai AFTER INSERT ON plans BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.name, '') || ' ' || COALESCE(NEW.goal_body, ''), 'plan', NEW.id, NEW.project_id);
END`,
	"search_index_plans_au": `CREATE TRIGGER IF NOT EXISTS search_index_plans_au AFTER UPDATE ON plans BEGIN
  DELETE FROM search_index WHERE entity_type = 'plan' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.name, '') || ' ' || COALESCE(NEW.goal_body, ''), 'plan', NEW.id, NEW.project_id);
END`,
	"search_index_plans_ad": `CREATE TRIGGER IF NOT EXISTS search_index_plans_ad AFTER DELETE ON plans BEGIN
  DELETE FROM search_index WHERE entity_type = 'plan' AND entity_id = OLD.id;
END`,
}

type searchIndexDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type searchIndexCheckFunc func(context.Context, searchIndexDB) (domain.SearchIndexIntegrityReport, error)

type reindexBackupHooks struct {
	Generation   exactGenerationHooks
	BeforeCommit func(int)
}

type searchCheckHooks struct {
	AfterCanonical func(int)
	BeforeFTS      func(int)
}

type searchConnectionControl struct {
	invalidate       func()
	validateIdentity func() error
}

const (
	searchIndexDetailLimit        = 100
	searchIndexUnsupportedType    = "<unsupported>"
	searchCheckGenerationAttempts = 3
)

func (s *Store) CheckSearchIndex(ctx context.Context) (domain.SearchIndexIntegrityReport, error) {
	return s.checkSearchIndexSnapshotWithHooks(ctx, searchCheckHooks{})
}

func (s *Store) checkSearchIndexSnapshotWithHooks(ctx context.Context, hooks searchCheckHooks) (domain.SearchIndexIntegrityReport, error) {
	conn, release, connection, err := s.acquireSearchConnection(ctx)
	if err != nil {
		return domain.SearchIndexIntegrityReport{}, err
	}
	defer release()
	transaction := transactionControl{invalidate: connection.invalidate, rollbackLabel: "rollback search transaction"}
	for attempt := 1; attempt <= searchCheckGenerationAttempts; attempt++ {
		beforeVersion, err := readDataVersion(ctx, conn, "read search data_version")
		if err != nil {
			return domain.SearchIndexIntegrityReport{}, err
		}
		var afterCanonical func()
		if hooks.AfterCanonical != nil {
			afterCanonical = func() { hooks.AfterCanonical(attempt) }
		}
		report, err := checkSearchIndexLogicalSnapshot(ctx, conn, afterCanonical, transaction)
		if err != nil {
			return report, err
		}
		if hooks.BeforeFTS != nil {
			hooks.BeforeFTS(attempt)
		}
		if err := attachSearchIndexFTSIntegrity(ctx, conn, &report); err != nil {
			return report, err
		}
		afterVersion, err := readDataVersion(ctx, conn, "read search data_version")
		if err != nil {
			return report, err
		}
		if afterVersion != beforeVersion {
			continue
		}
		if connection.validateIdentity != nil {
			if err := connection.validateIdentity(); err != nil {
				return domain.SearchIndexIntegrityReport{}, err
			}
		}
		return report, nil
	}
	return domain.SearchIndexIntegrityReport{}, errors.New("search index changed during every integrity check attempt")
}

func checkSearchIndexLogicalSnapshot(ctx context.Context, conn searchIndexDB, afterCanonical func(), control transactionControl) (report domain.SearchIndexIntegrityReport, returnErr error) {
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return domain.SearchIndexIntegrityReport{}, fmt.Errorf("begin search integrity snapshot: %w", err)
	}
	transactionOpen := true
	defer func() {
		if !transactionOpen {
			return
		}
		returnErr = errors.Join(returnErr, rollbackTransactionControlled(ctx, conn, control))
	}()
	report, err := checkSearchIndexLogical(ctx, conn, afterCanonical)
	if err != nil {
		return report, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return report, fmt.Errorf("commit search integrity snapshot: %w", err)
	}
	transactionOpen = false
	return report, nil
}

// ReindexSearchConfirmed applies the CLI's destructive-row confirmation
// policy inside the same transaction that performs repair, closing the gap
// between an earlier CLI preflight and deletion.
func (s *Store) ReindexSearchConfirmed(ctx context.Context, confirmed bool) (domain.SearchIndexReindexReport, error) {
	return s.reindexSearchWithPolicy(ctx, checkSearchIndex, confirmed)
}

func (s *Store) reindexSearchWithPolicy(ctx context.Context, check searchIndexCheckFunc, allowDiscard bool) (domain.SearchIndexReindexReport, error) {
	conn, release, connection, err := s.acquireSearchConnection(ctx)
	if err != nil {
		return domain.SearchIndexReindexReport{}, err
	}
	defer release()
	busyTimeoutMs := s.busyTimeoutMs
	if busyTimeoutMs <= 0 {
		busyTimeoutMs = kitBusyTimeoutMs()
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMs)); err != nil {
		return domain.SearchIndexReindexReport{}, fmt.Errorf("apply busy_timeout: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return domain.SearchIndexReindexReport{}, fmt.Errorf("begin immediate: %w", err)
	}
	return reindexSearchOnConn(ctx, conn, check, allowDiscard, transactionControl{
		invalidate:    connection.invalidate,
		rollbackLabel: "rollback search transaction",
	}, connection.validateIdentity)
}

func reindexSearchOnConn(ctx context.Context, conn searchIndexDB, check searchIndexCheckFunc, allowDiscard bool, transaction transactionControl, beforeCommit func() error) (result domain.SearchIndexReindexReport, returnErr error) {
	committed := false
	defer func() {
		if committed {
			return
		}
		returnErr = errors.Join(returnErr, rollbackTransactionControlled(ctx, conn, transaction))
	}()
	before, err := check(ctx, conn)
	result.Before = before
	if err != nil {
		return result, fmt.Errorf("pre-reindex check: %w", err)
	}
	result.BackupRecommended = before.RequiresBackupBeforeRepair()
	if result.BackupRecommended && !allowDiscard {
		return result, domain.NewError(domain.ErrValidation, "confirmation required before discarding existing search-index evidence", map[string]any{
			"requires_confirmation": true,
			"report":                before,
		})
	}
	triggerNames, err := searchTriggerNames(ctx, conn)
	if err != nil {
		return result, err
	}
	for _, name := range triggerNames {
		if _, err := conn.ExecContext(ctx, `DROP TRIGGER `+quoteSQLiteIdentifier(name)); err != nil {
			return result, errors.New("drop search-index trigger failed")
		}
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM search_index`); err != nil {
		return result, fmt.Errorf("clear search index: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO search_index(search_index) VALUES('rebuild')`); err != nil {
		return result, fmt.Errorf("reset search index internals: %w", err)
	}
	if _, err := conn.ExecContext(ctx, canonicalSearchSourceCTE+`
INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT content, entity_type, entity_id, project_id FROM canonical`); err != nil {
		return result, fmt.Errorf("rebuild search index: %w", err)
	}
	canonicalNames := make([]string, 0, len(canonicalSearchTriggers))
	for name := range canonicalSearchTriggers {
		canonicalNames = append(canonicalNames, name)
	}
	sort.Strings(canonicalNames)
	for _, name := range canonicalNames {
		if _, err := conn.ExecContext(ctx, canonicalSearchTriggers[name]); err != nil {
			return result, fmt.Errorf("create search trigger %s: %w", name, err)
		}
	}
	after, err := check(ctx, conn)
	result.After = after
	if err != nil {
		return result, fmt.Errorf("post-reindex check: %w", err)
	}
	if !after.Healthy {
		return result, domain.NewError(domain.ErrSearchIndexInvalid, "search index remains invalid after rebuild", map[string]any{"report": after})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if beforeCommit != nil {
		if err := beforeCommit(); err != nil {
			return result, err
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return result, fmt.Errorf("commit search reindex: %w", err)
	}
	committed = true
	return result, nil
}

// ReindexSearchConfirmedWithBackup retries until the retained backup and the
// BEGIN IMMEDIATE transaction refer to one externally stable data_version.
// Lease validation pins both the lock and every generated recovery pathname.
func (s *Store) ReindexSearchConfirmedWithBackup(
	ctx context.Context,
	createBackup MaintenanceBackupCreator,
	discardBackup func(string) error,
	validateLease func() error,
) (domain.SearchIndexReindexReport, string, error) {
	return s.reindexSearchConfirmedWithBackup(ctx, createBackup, discardBackup, validateLease, reindexBackupHooks{})
}

func (s *Store) reindexSearchConfirmedWithBackup(
	ctx context.Context,
	createBackup MaintenanceBackupCreator,
	discardBackup func(string) error,
	validateLease func() error,
	hooks reindexBackupHooks,
) (domain.SearchIndexReindexReport, string, error) {
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.maintenanceConn == nil {
		return domain.SearchIndexReindexReport{}, "", errors.New("confirmed backup reindex requires OpenSearchMaintenance")
	}
	if createBackup == nil || discardBackup == nil || validateLease == nil {
		return domain.SearchIndexReindexReport{}, "", errors.New("confirmed backup reindex requires backup create, discard, and lease validation callbacks")
	}
	if err := s.validateMaintenanceIdentity(); err != nil {
		return domain.SearchIndexReindexReport{}, "", err
	}
	if err := validateLease(); err != nil {
		return domain.SearchIndexReindexReport{}, "", err
	}
	conn := s.maintenanceConn
	connection := s.maintenanceSearchConnectionControl(conn)
	transaction := transactionControl{
		invalidate:    connection.invalidate,
		rollbackLabel: "rollback search transaction",
	}
	busyTimeoutMs := s.busyTimeoutMs
	if busyTimeoutMs <= 0 {
		busyTimeoutMs = kitBusyTimeoutMs()
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMs)); err != nil {
		return domain.SearchIndexReindexReport{}, "", fmt.Errorf("apply busy_timeout: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return domain.SearchIndexReindexReport{}, "", fmt.Errorf("begin reconstructive policy check: %w", err)
	}
	policyReport, err := checkSearchIndex(ctx, conn)
	if err != nil {
		primaryErr := fmt.Errorf("reconstructive policy check: %w", err)
		return domain.SearchIndexReindexReport{}, "", errors.Join(primaryErr, rollbackTransactionControlled(ctx, conn, transaction))
	}
	if !policyReport.RequiresBackupBeforeRepair() {
		result, err := reindexSearchOnConn(ctx, conn, checkSearchIndex, true, transaction, connection.validateIdentity)
		return result, "", err
	}
	if err := rollbackTransactionControlled(ctx, conn, transaction); err != nil {
		return domain.SearchIndexReindexReport{}, "", fmt.Errorf("end destructive policy check: %w", err)
	}

	validateLeaseAndIdentity := func() error {
		if err := validateLease(); err != nil {
			return err
		}
		return s.validateMaintenanceIdentity()
	}
	backupPath, attempt, err := prepareExactGeneration(ctx, conn, searchReindexExactGenerationPolicy, exactGenerationConfig{
		create:  createBackup,
		discard: discardBackup,
		snapshot: func(snapshotCtx context.Context, destinationPath string) error {
			return snapshotWithExecutor(snapshotCtx, conn, destinationPath, false, snapshotHooks{})
		},
		beforeSnapshot: s.validateMaintenanceIdentity,
		afterSnapshot:  validateLeaseAndIdentity,
		hooks:          hooks.Generation,
		transaction:    transaction,
	})
	if err != nil {
		return domain.SearchIndexReindexReport{}, backupPath, err
	}

	leaseOrIdentityInvalid := false
	beforeCommit := func() error {
		if hooks.BeforeCommit != nil {
			hooks.BeforeCommit(attempt)
		}
		err := errors.Join(s.validateMaintenanceIdentity(), validateLease())
		leaseOrIdentityInvalid = err != nil
		return err
	}
	result, err := reindexSearchOnConn(ctx, conn, checkSearchIndex, true, transaction, beforeCommit)
	if leaseOrIdentityInvalid && !errors.Is(err, errRollbackUnproven) {
		if discardErr := discardBackup(backupPath); discardErr != nil {
			return result, backupPath, errors.Join(err, fmt.Errorf("discard stale reindex backup: %w", discardErr))
		}
		return result, "", err
	}
	return result, backupPath, err
}

func (s *Store) acquireSearchMaintenanceConn(ctx context.Context) (*sql.Conn, func(), error) {
	conn, release, _, err := s.acquireSearchConnection(ctx)
	return conn, release, err
}

func (s *Store) acquireSearchConnection(ctx context.Context) (*sql.Conn, func(), searchConnectionControl, error) {
	s.maintenanceMu.Lock()
	if s.maintenanceConn != nil {
		if err := s.validateMaintenanceIdentity(); err != nil {
			s.maintenanceMu.Unlock()
			return nil, nil, searchConnectionControl{}, err
		}
		conn := s.maintenanceConn
		return conn, s.maintenanceMu.Unlock, s.maintenanceSearchConnectionControl(conn), nil
	}
	if s.maintenancePath != "" {
		s.maintenanceMu.Unlock()
		return nil, nil, searchConnectionControl{}, maintenanceValidationError("maintenance database connection is unavailable")
	}
	s.maintenanceMu.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, nil, searchConnectionControl{}, err
	}
	return conn, func() { _ = conn.Close() }, searchConnectionControl{
		invalidate: func() { invalidateSQLiteConn(conn) },
	}, nil
}

func (s *Store) maintenanceSearchConnectionControl(conn *sql.Conn) searchConnectionControl {
	return searchConnectionControl{
		invalidate: func() {
			invalidateSQLiteConn(conn)
			if s.maintenanceConn == conn {
				s.maintenanceConn = nil
			}
		},
		validateIdentity: s.validateMaintenanceIdentity,
	}
}

func (s *Store) validateMaintenanceIdentity() error {
	if s.maintenancePath == "" || s.maintenanceIdentity == nil {
		return maintenanceValidationError("maintenance database identity is unavailable")
	}
	_, current, err := validateMaintenancePath(s.maintenancePath)
	if err != nil || !os.SameFile(s.maintenanceIdentity, current) {
		return maintenanceValidationError("database file changed after maintenance open")
	}
	return nil
}

func checkSearchIndex(ctx context.Context, db searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
	report, err := checkSearchIndexLogical(ctx, db, nil)
	if err != nil {
		return report, err
	}
	if err := attachSearchIndexFTSIntegrity(ctx, db, &report); err != nil {
		return report, err
	}
	return report, nil
}

func checkSearchIndexLogical(ctx context.Context, db searchIndexDB, afterCanonical func()) (domain.SearchIndexIntegrityReport, error) {
	report := domain.SearchIndexIntegrityReport{}
	if err := prepareSearchCheckTablesWithHook(ctx, db, afterCanonical); err != nil {
		return report, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = cleanupSearchCheckTables(cleanupCtx, db)
	}()
	types := make(map[string]*domain.SearchIndexTypeReport)
	for _, entityType := range domain.AllSearchEntityTypes() {
		name := string(entityType)
		typeReport := newSearchIndexTypeReport(name)
		types[name] = &typeReport
	}
	getType := func(name string) *domain.SearchIndexTypeReport {
		if typeReport, ok := types[name]; ok {
			return typeReport
		}
		typeReport := newSearchIndexTypeReport(name)
		types[name] = &typeReport
		return &typeReport
	}
	rows, err := db.QueryContext(ctx, `
SELECT 'source', entity_type, COUNT(*) FROM search_check_canonical GROUP BY entity_type
UNION ALL
SELECT 'index', entity_type, COUNT(*) FROM search_check_index GROUP BY entity_type`)
	if err != nil {
		return report, fmt.Errorf("search index totals: %w", err)
	}
	for rows.Next() {
		var side, entityType string
		var count int64
		if err := rows.Scan(&side, &entityType, &count); err != nil {
			_ = rows.Close()
			return report, err
		}
		if side == "source" {
			getType(entityType).SourceTotal = count
			report.SourceTotal += count
		} else {
			getType(entityType).IndexTotal = count
			report.IndexTotal += count
		}
	}
	if err := rows.Close(); err != nil {
		return report, err
	}
	detailRows, err := db.QueryContext(ctx, `WITH issues AS (`+searchIndexIssueRowsSQL+`), ranked AS (
SELECT issues.*, ROW_NUMBER() OVER (
  PARTITION BY kind, entity_type ORDER BY entity_id, project_id, indexed_project_id
) AS sample_rank, COUNT(*) OVER (PARTITION BY kind, entity_type) AS issue_count FROM issues
)
SELECT kind, entity_type, entity_id, project_id, indexed_project_id,
       entity_type_storage, entity_id_storage, project_id_storage, issue_count
  FROM ranked WHERE sample_rank <= ?
 ORDER BY kind, entity_type, sample_rank`, searchIndexDetailLimit)
	if err != nil {
		return report, fmt.Errorf("search index issue details: %w", err)
	}
	for detailRows.Next() {
		var kind, entityType, entityTypeStorage, entityIDStorage, projectIDStorage string
		var entityID, projectID, indexedProjectID, count int64
		if err := detailRows.Scan(&kind, &entityType, &entityID, &projectID, &indexedProjectID, &entityTypeStorage, &entityIDStorage, &projectIDStorage, &count); err != nil {
			_ = detailRows.Close()
			return report, err
		}
		typeReport := getType(entityType)
		setSearchIssueCount(typeReport, kind, count)
		appendSearchIssueDetail(typeReport, kind, entityID, projectID, indexedProjectID, entityTypeStorage, entityIDStorage, projectIDStorage)
	}
	if err := detailRows.Close(); err != nil {
		return report, err
	}
	triggerReport, err := checkSearchTriggers(ctx, db)
	if err != nil {
		return report, err
	}
	report.Triggers = triggerReport
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		left, right := searchTypeOrder(names[i]), searchTypeOrder(names[j])
		if left != right {
			return left < right
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		typeReport := types[name]
		setSearchIssueTruncation(typeReport)
		report.Types = append(report.Types, *typeReport)
	}
	return report, nil
}

func attachSearchIndexFTSIntegrity(ctx context.Context, db searchIndexDB, report *domain.SearchIndexIntegrityReport) error {
	report.FTS5 = domain.SearchIndexFTSIntegrity{OK: true}
	if _, err := db.ExecContext(ctx, `INSERT INTO search_index(search_index) VALUES('integrity-check')`); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if isSQLiteBusyOrLocked(err) {
			return fmt.Errorf("run FTS5 integrity-check: %w", err)
		}
		report.FTS5 = domain.SearchIndexFTSIntegrity{OK: false, Error: "FTS5 integrity-check failed"}
	}
	report.Healthy = report.IsHealthy()
	return nil
}

func prepareSearchCheckTablesWithHook(ctx context.Context, db searchIndexDB, afterCanonical func()) error {
	if err := cleanupSearchCheckTables(ctx, db); err != nil {
		return err
	}
	statements := []string{
		`CREATE TEMP TABLE search_check_canonical(content TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id INTEGER NOT NULL, project_id INTEGER NOT NULL)`,
		canonicalSearchSourceCTE + `INSERT INTO search_check_canonical SELECT content, entity_type, entity_id, project_id FROM canonical`,
		`CREATE INDEX temp.search_check_canonical_key ON search_check_canonical(entity_type, entity_id)`,
		`CREATE TEMP TABLE search_check_index AS
SELECT rowid AS index_rowid, content,
       CASE
         WHEN typeof(entity_type) = 'text' AND entity_type IN ('task','comment','error','solution','plan') THEN entity_type
         WHEN typeof(entity_type) = 'text' THEN '<unsupported>'
         WHEN typeof(entity_type) = 'null' THEN '<null>'
         ELSE '<malformed>'
       END AS entity_type,
		CAST(CASE typeof(entity_id) WHEN 'integer' THEN entity_id ELSE 0 END AS INTEGER) AS entity_id,
       CASE typeof(project_id) WHEN 'integer' THEN project_id ELSE 0 END AS project_id,
       typeof(entity_type) AS entity_type_storage,
       typeof(entity_id) AS entity_id_storage,
       typeof(project_id) AS project_id_storage,
       typeof(entity_type) = 'text' AND typeof(entity_id) = 'integer' AND typeof(project_id) = 'integer' AS metadata_valid,
       typeof(entity_type) = 'text' AND entity_type IN ('task','comment','error','solution','plan') AS supported
  FROM search_index`,
		`CREATE INDEX temp.search_check_index_key ON search_check_index(entity_type, entity_id)`,
		`CREATE INDEX temp.search_check_index_flags ON search_check_index(metadata_valid, supported, entity_type, entity_id)`,
	}
	for index, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("prepare search integrity snapshot: %w", err)
		}
		if index == 1 && afterCanonical != nil {
			afterCanonical()
		}
	}
	return nil
}

func cleanupSearchCheckTables(ctx context.Context, db searchIndexDB) error {
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS temp.search_check_index`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS temp.search_check_canonical`); err != nil {
		return err
	}
	return nil
}

func setSearchIssueCount(report *domain.SearchIndexTypeReport, kind string, count int64) {
	switch kind {
	case "missing":
		report.Missing.Count = count
	case "orphaned":
		report.Orphaned.Count = count
	case "unsupported":
		report.Unsupported.Count = count
	case "malformed":
		report.Malformed.Count = count
	case "duplicates":
		report.Duplicates.Count = count
	case "content":
		report.ContentMismatched.Count = count
	case "project":
		report.ProjectMismatched.Count = count
	}
}

func appendSearchIssueDetail(report *domain.SearchIndexTypeReport, kind string, entityID, projectID, indexedProjectID int64, entityTypeStorage, entityIDStorage, projectIDStorage string) {
	ref := domain.SearchIndexRowRef{EntityType: report.EntityType, EntityID: entityID, ProjectID: projectID}
	switch kind {
	case "missing":
		report.Missing.Details = append(report.Missing.Details, ref)
	case "orphaned":
		report.Orphaned.Details = append(report.Orphaned.Details, ref)
	case "unsupported":
		report.Unsupported.Details = append(report.Unsupported.Details, ref)
	case "malformed":
		report.Malformed.Details = append(report.Malformed.Details, domain.SearchIndexMalformed{EntityType: report.EntityType, EntityID: entityID, ProjectID: projectID, EntityTypeStorage: entityTypeStorage, EntityIDStorage: entityIDStorage, ProjectIDStorage: projectIDStorage})
	case "duplicates":
		report.Duplicates.Details = append(report.Duplicates.Details, domain.SearchIndexDuplicate{EntityType: report.EntityType, EntityID: entityID, IndexCount: indexedProjectID})
	case "content":
		report.ContentMismatched.Details = append(report.ContentMismatched.Details, ref)
	case "project":
		report.ProjectMismatched.Details = append(report.ProjectMismatched.Details, domain.SearchIndexProjectMismatch{EntityType: report.EntityType, EntityID: entityID, SourceProjectID: projectID, IndexedProjectID: indexedProjectID})
	}
}

func setSearchIssueTruncation(report *domain.SearchIndexTypeReport) {
	report.Missing.Truncated = report.Missing.Count > int64(len(report.Missing.Details))
	report.Orphaned.Truncated = report.Orphaned.Count > int64(len(report.Orphaned.Details))
	report.Unsupported.Truncated = report.Unsupported.Count > int64(len(report.Unsupported.Details))
	report.Malformed.Truncated = report.Malformed.Count > int64(len(report.Malformed.Details))
	report.Duplicates.Truncated = report.Duplicates.Count > int64(len(report.Duplicates.Details))
	report.ContentMismatched.Truncated = report.ContentMismatched.Count > int64(len(report.ContentMismatched.Details))
	report.ProjectMismatched.Truncated = report.ProjectMismatched.Count > int64(len(report.ProjectMismatched.Details))
}

func checkSearchTriggers(ctx context.Context, db searchIndexDB) (domain.SearchIndexTriggerReport, error) {
	report := domain.SearchIndexTriggerReport{ExpectedCount: len(canonicalSearchTriggers), Missing: []string{}, Stale: []string{}}
	catalog, err := searchTriggerCatalog(ctx, db)
	if err != nil {
		return report, err
	}
	actual := make(map[string]string)
	for _, trigger := range catalog {
		if trigger.prefixed || triggerSQLTargetsSearchIndex(trigger.definition) {
			actual[trigger.name] = trigger.definition
		}
	}
	report.ActualCount = len(actual)
	for name, expected := range canonicalSearchTriggers {
		definition, ok := actual[name]
		if !ok {
			report.Missing = append(report.Missing, name)
			continue
		}
		if normalizeTriggerSQL(definition) != normalizeTriggerSQL(expected) {
			report.Stale = append(report.Stale, name)
		}
	}
	for name := range actual {
		if _, ok := canonicalSearchTriggers[name]; !ok {
			report.UnexpectedCount++
		}
	}
	sort.Strings(report.Missing)
	sort.Strings(report.Stale)
	return report, nil
}

func newSearchIndexTypeReport(name string) domain.SearchIndexTypeReport {
	return domain.SearchIndexTypeReport{
		EntityType:        name,
		Missing:           domain.SearchIndexIssueSet[domain.SearchIndexRowRef]{Details: []domain.SearchIndexRowRef{}},
		Orphaned:          domain.SearchIndexIssueSet[domain.SearchIndexRowRef]{Details: []domain.SearchIndexRowRef{}},
		Unsupported:       domain.SearchIndexIssueSet[domain.SearchIndexRowRef]{Details: []domain.SearchIndexRowRef{}},
		Malformed:         domain.SearchIndexIssueSet[domain.SearchIndexMalformed]{Details: []domain.SearchIndexMalformed{}},
		Duplicates:        domain.SearchIndexIssueSet[domain.SearchIndexDuplicate]{Details: []domain.SearchIndexDuplicate{}},
		ContentMismatched: domain.SearchIndexIssueSet[domain.SearchIndexRowRef]{Details: []domain.SearchIndexRowRef{}},
		ProjectMismatched: domain.SearchIndexIssueSet[domain.SearchIndexProjectMismatch]{Details: []domain.SearchIndexProjectMismatch{}},
	}
}

func searchTriggerNames(ctx context.Context, db searchIndexDB) ([]string, error) {
	catalog, err := searchTriggerCatalog(ctx, db)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, trigger := range catalog {
		if trigger.prefixed || triggerSQLTargetsSearchIndex(trigger.definition) {
			names = append(names, trigger.name)
		}
	}
	return names, nil
}

type searchTriggerCatalogEntry struct {
	name       string
	definition string
	prefixed   bool
}

func searchTriggerCatalog(ctx context.Context, db searchIndexDB) ([]searchTriggerCatalogEntry, error) {
	rows, err := db.QueryContext(ctx, `
SELECT name, COALESCE(sql, ''), name GLOB 'search_index_*'
  FROM sqlite_master
 WHERE type = 'trigger'
 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("search index triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var catalog []searchTriggerCatalogEntry
	for rows.Next() {
		var trigger searchTriggerCatalogEntry
		if err := rows.Scan(&trigger.name, &trigger.definition, &trigger.prefixed); err != nil {
			return nil, err
		}
		catalog = append(catalog, trigger)
	}
	return catalog, rows.Err()
}

func triggerSQLTargetsSearchIndex(definition string) bool {
	tokens := sqliteSQLIdentifiers(definition)
	start := len(tokens)
	for index, token := range tokens {
		if token == "begin" {
			start = index + 1
			break
		}
	}
	for index := start; index < len(tokens); index++ {
		var target int
		switch tokens[index] {
		case "insert", "replace":
			target = index + 1
			if target < len(tokens) && tokens[target] == "or" {
				target += 2
			}
			if target >= len(tokens) || tokens[target] != "into" {
				continue
			}
			target++
		case "delete":
			target = index + 1
			if target >= len(tokens) || tokens[target] != "from" {
				continue
			}
			target++
		case "update":
			target = index + 1
			if target < len(tokens) && tokens[target] == "or" {
				target += 2
			}
		default:
			continue
		}
		if target < len(tokens) && (tokens[target] == "search_index" || strings.HasPrefix(tokens[target], "search_index_")) {
			return true
		}
	}
	return false
}

func sqliteSQLIdentifiers(statement string) []string {
	runes := []rune(statement)
	identifiers := make([]string, 0, len(runes)/8)
	for index := 0; index < len(runes); {
		switch {
		case runes[index] == '-' && index+1 < len(runes) && runes[index+1] == '-':
			index += 2
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
		case runes[index] == '/' && index+1 < len(runes) && runes[index+1] == '*':
			index += 2
			for index+1 < len(runes) && (runes[index] != '*' || runes[index+1] != '/') {
				index++
			}
			if index+1 < len(runes) {
				index += 2
			}
		case runes[index] == '\'':
			index++
			for index < len(runes) {
				if runes[index] != '\'' {
					index++
					continue
				}
				index++
				if index < len(runes) && runes[index] == '\'' {
					index++
					continue
				}
				break
			}
		case runes[index] == '"' || runes[index] == '`' || runes[index] == '[':
			opening := runes[index]
			closing := opening
			if opening == '[' {
				closing = ']'
			}
			index++
			start := index
			for index < len(runes) && runes[index] != closing {
				index++
			}
			if index > start {
				identifiers = append(identifiers, strings.ToLower(string(runes[start:index])))
			}
			if index < len(runes) {
				index++
			}
		case isSQLiteIdentifierRune(runes[index]):
			start := index
			for index < len(runes) && isSQLiteIdentifierRune(runes[index]) {
				index++
			}
			identifiers = append(identifiers, strings.ToLower(string(runes[start:index])))
		default:
			index++
		}
	}
	return identifiers
}

func isSQLiteIdentifierRune(value rune) bool {
	return value == '_' || value == '$' || unicode.IsLetter(value) || unicode.IsDigit(value)
}

func quoteSQLiteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func normalizeTriggerSQL(definition string) string {
	definition = strings.TrimSuffix(strings.TrimSpace(definition), ";")
	var normalized strings.Builder
	var quote rune
	for _, r := range definition {
		if quote != 0 {
			normalized.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
			normalized.WriteRune(r)
		case ' ', '\t', '\r', '\n':
		default:
			normalized.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Replace(normalized.String(), "createtriggerifnotexists", "createtrigger", 1)
}

func searchTypeOrder(name string) int {
	for index, entityType := range domain.AllSearchEntityTypes() {
		if name == string(entityType) {
			return index
		}
	}
	return len(domain.AllSearchEntityTypes())
}

func isSQLiteBusyOrLocked(err error) bool {
	var sqliteErr *modernsqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	primaryCode := sqliteErr.Code() & 0xff
	return primaryCode == sqlite3.SQLITE_BUSY || primaryCode == sqlite3.SQLITE_LOCKED
}
