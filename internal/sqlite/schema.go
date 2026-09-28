package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"unicode"
)

// schemaSQL is the sole authoritative SQLite schema baseline. Existing
// databases are compared with a fresh in-memory instance of this baseline;
// validation therefore cannot drift from the SQL used for initialization.
//
//go:embed schema.sql
var schemaSQL string

const v030SchemaMigrationsSQL = `CREATE TABLE schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

// v030ReleaseSchemaFingerprint is SHA-256 of the semantic schema materialized
// from the immutable v0.30.0 migration chain at release commit d710fdf.
const v030ReleaseSchemaFingerprint = "df4f869cbdcee6b3171b3d3cbe7e246d40c7202d40fa060abf892eef39e9bfad"

var v030MigrationVersions = []string{
	"001_initial.sql",
	"002_entities.sql",
	"003_activity_logs.sql",
	"004_tags.sql",
	"005_transition_guards.sql",
	"006_comment_tags.sql",
	"007_errors.sql",
	"008_solution_likes.sql",
	"009_events.sql",
	"010_agent_attribution.sql",
	"011_purge_tui_summary_pollution.sql",
	"012_task_state.sql",
	"013_bucket_permissions_operations.sql",
	"014_workflow_defaults.sql",
	"015_priority_id.sql",
	"016_severity_id.sql",
	"017_drop_priority_severity_defaults.sql",
	"018_drop_legacy_event_payloads.sql",
	"019_unify_tool_call_events.sql",
	"020_drop_config_tables.sql",
	"021_rebind_orphan_buckets.sql",
	"022_search_index.sql",
	"023_plans.sql",
	"024_search_index_plans.sql",
	"025_projects_cascade.sql",
	"026_tasks_parent_id.sql",
	"027_tasks_parent_project_fk.sql",
	"028_tasks_depth.sql",
	"029_repair_tasks_depth.sql",
	"030_rename_errors_researched.sql",
	"031_notes.sql",
	"032_events_comment_log.sql",
	"033_drop_context_entries.sql",
	"034_realtime_read_path_indexes.sql",
	"035_events_order_by_indexes.sql",
}

func applyCurrentSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("initialize current schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit current schema: %w", err)
	}
	return nil
}

func currentSchemaFingerprint(ctx context.Context) (string, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return "", err
	}
	return schemaFingerprint(ctx, db)
}

func schemaFingerprint(ctx context.Context, db schemaQueryer) (string, error) {
	return schemaFingerprintOptions(ctx, db, false, true)
}

func semanticSchemaFingerprint(ctx context.Context, db schemaQueryer) (string, error) {
	return schemaFingerprintOptions(ctx, db, true, false)
}

func schemaFingerprintOptions(ctx context.Context, db schemaQueryer, excludeMigrations, includeUserVersion bool) (string, error) {
	var fingerprint strings.Builder
	rows, err := db.QueryContext(ctx, `
		SELECT type, name, tbl_name, COALESCE(sql, '')
		FROM sqlite_master
		ORDER BY type, name`)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var objectType, name, tableName, definition string
		if err := rows.Scan(&objectType, &name, &tableName, &definition); err != nil {
			return "", err
		}
		if excludeMigrations && strings.EqualFold(objectType, "table") && strings.EqualFold(name, "schema_migrations") {
			continue
		}
		if excludeMigrations && strings.EqualFold(objectType, "index") && strings.HasPrefix(strings.ToLower(name), "sqlite_autoindex_schema_migrations_") {
			continue
		}
		fmt.Fprintf(&fingerprint, "%s\x00%s\x00%s\x00%s\x00", strings.ToLower(objectType), strings.ToLower(name), strings.ToLower(tableName), normalizeSchemaDefinition(definition))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if includeUserVersion {
		var userVersion int
		if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
			return "", err
		}
		fmt.Fprintf(&fingerprint, "user_version=%d", userVersion)
	}
	return fingerprint.String(), nil
}

func normalizeSchemaDefinition(definition string) string {
	// SQLite stores the original CREATE statement. Canonicalize tokens rather
	// than lowercasing the whole string: trigger literals are schema semantics,
	// not formatting, so changing 'task' to 'TASK' must remain detectable.
	tokens := scanSchemaTokens(definition)
	canonical := make([]string, 0, len(tokens))
	for index := 0; index < len(tokens); index++ {
		if index+2 < len(tokens) && tokens[index].kind == schemaBareToken &&
			tokens[index].text == "if" && tokens[index+1].kind == schemaBareToken &&
			tokens[index+1].text == "not" && tokens[index+2].kind == schemaBareToken &&
			tokens[index+2].text == "exists" {
			index += 2
			continue
		}
		canonical = append(canonical, tokens[index].text)
	}
	return strings.Join(canonical, "\x1f")
}

type v030BridgeHooks struct {
	afterSearchRebuild func(context.Context, searchIndexDB) error
	afterCommit        func()
}

func bridgeV030ReleaseDatabase(ctx context.Context, db *sql.DB) error {
	return bridgeV030ReleaseDatabaseWithHooks(ctx, db, v030BridgeHooks{})
}

func bridgeV030ReleaseDatabaseWithHooks(ctx context.Context, db *sql.DB, hooks v030BridgeHooks) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return maintenanceValidationError("database connection could not be pinned for the v0.30.0 bridge")
	}
	defer func() { _ = conn.Close() }()

	if err := validateV030ReleaseDatabase(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin v0.30.0 database bridge: %w", err)
	}
	inTransaction := true
	defer func() {
		if inTransaction {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	// The source was checked before acquiring the writer lock. Repeat every
	// check while holding it so no external writer can change the identity
	// between validation and the two intentional metadata statements.
	if err := validateV030ReleaseDatabase(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "DROP TABLE schema_migrations"); err != nil {
		return fmt.Errorf("remove v0.30.0 migration marker: %w", err)
	}
	if _, err := conn.ExecContext(ctx, documentMetadataSQL()); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		return fmt.Errorf("set current schema marker: %w", err)
	}
	if err := verifyCurrentOmakitenSchema(ctx, conn); err != nil {
		return fmt.Errorf("verify v0.30.0 bridge before commit: %w", err)
	}
	if err := rebuildAndVerifyV030SearchIndex(ctx, conn, hooks); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit v0.30.0 database bridge: %w", err)
	}
	inTransaction = false
	if hooks.afterCommit != nil {
		hooks.afterCommit()
	}
	return nil
}

func rebuildAndVerifyV030SearchIndex(ctx context.Context, conn searchIndexDB, hooks v030BridgeHooks) error {
	if err := rebuildSearchIndex(ctx, conn); err != nil {
		return fmt.Errorf("rebuild v0.30.0 search index: %w", err)
	}
	if hooks.afterSearchRebuild != nil {
		if err := hooks.afterSearchRebuild(ctx, conn); err != nil {
			return fmt.Errorf("after v0.30.0 search rebuild: %w", err)
		}
	}
	searchReport, err := checkSearchIndex(ctx, conn)
	if err != nil {
		return fmt.Errorf("verify v0.30.0 search index before commit: %w", err)
	}
	if !searchReport.Healthy || !searchReport.FTS5.OK {
		return fmt.Errorf("verify v0.30.0 search index before commit: index remains invalid")
	}
	return nil
}

func validateV030ReleaseDatabase(ctx context.Context, db schemaQueryer) error {
	var userVersion int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		return v030ReleaseValidationError("v0.30.0 database marker cannot be inspected")
	}
	if userVersion != 0 {
		return v030ReleaseValidationError("database is not the exact v0.30.0 release shape")
	}

	var objectType, name, tableName, definition string
	err := db.QueryRowContext(ctx, `
		SELECT type, name, tbl_name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE lower(name) = 'schema_migrations'`).Scan(&objectType, &name, &tableName, &definition)
	if err != nil || !strings.EqualFold(objectType, "table") || !strings.EqualFold(name, "schema_migrations") || !strings.EqualFold(tableName, "schema_migrations") ||
		normalizeSchemaDefinition(definition) != normalizeSchemaDefinition(v030SchemaMigrationsSQL) {
		return v030ReleaseValidationError("database is not the exact v0.30.0 release shape")
	}
	if err := validateV030MigrationColumns(ctx, db); err != nil {
		return err
	}
	if err := validateV030MigrationVersions(ctx, db); err != nil {
		return err
	}
	expected, err := releaseSchemaSemanticFingerprint(ctx)
	if err != nil {
		return v030ReleaseValidationError("current schema baseline is unavailable")
	}
	actual, err := semanticSchemaFingerprint(ctx, db)
	if err != nil || actual != expected || fmt.Sprintf("%x", sha256.Sum256([]byte(actual))) != v030ReleaseSchemaFingerprint {
		return v030ReleaseValidationError("database is not the exact v0.30.0 release shape")
	}
	return nil
}

func v030ReleaseValidationError(reason string) error {
	return maintenanceValidationError(reason + "; use a new database path or restore a current-compatible backup")
}

func validateV030MigrationColumns(ctx context.Context, db schemaQueryer) error {
	rows, err := db.QueryContext(ctx, `
		SELECT cid, name, type, "notnull", COALESCE(dflt_value, ''), pk
		FROM pragma_table_info('schema_migrations')
		ORDER BY cid`)
	if err != nil {
		return v030ReleaseValidationError("v0.30.0 migration history cannot be inspected")
	}
	defer func() { _ = rows.Close() }()
	type column struct {
		cid, notNull, primaryKey int
		name, typ, defaultValue  string
	}
	var columns []column
	for rows.Next() {
		var item column
		if err := rows.Scan(&item.cid, &item.name, &item.typ, &item.notNull, &item.defaultValue, &item.primaryKey); err != nil {
			return v030ReleaseValidationError("v0.30.0 migration history cannot be inspected")
		}
		columns = append(columns, item)
	}
	if err := rows.Err(); err != nil || len(columns) != 2 {
		return v030ReleaseValidationError("database is not the exact v0.30.0 release shape")
	}
	want := []column{
		{cid: 0, name: "version", typ: "TEXT", notNull: 0, defaultValue: "", primaryKey: 1},
		{cid: 1, name: "applied_at", typ: "TEXT", notNull: 1, defaultValue: "CURRENT_TIMESTAMP", primaryKey: 0},
	}
	for index := range want {
		if columns[index] != want[index] {
			return v030ReleaseValidationError("database is not the exact v0.30.0 release shape")
		}
	}
	return nil
}

func validateV030MigrationVersions(ctx context.Context, db schemaQueryer) error {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return v030ReleaseValidationError("v0.30.0 migration history cannot be inspected")
	}
	defer func() { _ = rows.Close() }()
	index := 0
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil || index >= len(v030MigrationVersions) || version != v030MigrationVersions[index] {
			return v030ReleaseValidationError("database is not the exact v0.30.0 release shape")
		}
		index++
	}
	if err := rows.Err(); err != nil || index != len(v030MigrationVersions) {
		return v030ReleaseValidationError("database is not the exact v0.30.0 release shape")
	}
	return nil
}

type schemaTokenKind uint8

const (
	schemaBareToken schemaTokenKind = iota
	schemaLiteralToken
	schemaIdentifierToken
)

type schemaToken struct {
	kind schemaTokenKind
	text string
}

func scanSchemaTokens(definition string) []schemaToken {
	runes := []rune(definition)
	tokens := make([]schemaToken, 0, len(runes)/2)
	for index := 0; index < len(runes); {
		if unicode.IsSpace(runes[index]) {
			index++
			continue
		}
		if next, ok := skipSchemaComment(runes, index); ok {
			index = next
			continue
		}
		var token schemaToken
		switch {
		case runes[index] == '\'':
			token, index = scanSchemaLiteral(runes, index)
		case runes[index] == '"' || runes[index] == '`':
			token, index = scanSchemaQuotedIdentifier(runes, index)
		case runes[index] == '[':
			token, index = scanSchemaBracketIdentifier(runes, index)
		case isSchemaTokenRune(runes[index]):
			token, index = scanSchemaBareToken(runes, index)
		default:
			token = schemaToken{text: string(runes[index])}
			index++
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func skipSchemaComment(runes []rune, index int) (int, bool) {
	if index+1 >= len(runes) {
		return index, false
	}
	if runes[index] == '-' && runes[index+1] == '-' {
		index += 2
		for index < len(runes) && runes[index] != '\n' {
			index++
		}
		return index, true
	}
	if runes[index] != '/' || runes[index+1] != '*' {
		return index, false
	}
	index += 2
	for index+1 < len(runes) && (runes[index] != '*' || runes[index+1] != '/') {
		index++
	}
	if index+1 < len(runes) {
		index += 2
	}
	return index, true
}

func scanSchemaLiteral(runes []rune, index int) (schemaToken, int) {
	start := index
	index++
	for index < len(runes) {
		if runes[index] != '\'' {
			index++
			continue
		}
		if index+1 < len(runes) && runes[index+1] == '\'' {
			index += 2
			continue
		}
		index++
		break
	}
	return schemaToken{kind: schemaLiteralToken, text: string(runes[start:index])}, index
}

func scanSchemaQuotedIdentifier(runes []rune, index int) (schemaToken, int) {
	quote := runes[index]
	index++
	start := index
	var value strings.Builder
	for index < len(runes) {
		if runes[index] != quote {
			index++
			continue
		}
		if index+1 < len(runes) && runes[index+1] == quote {
			value.WriteString(string(runes[start:index]))
			value.WriteRune(quote)
			index += 2
			start = index
			continue
		}
		value.WriteString(string(runes[start:index]))
		index++
		break
	}
	return schemaToken{kind: schemaIdentifierToken, text: strings.ToLower(value.String())}, index
}

func scanSchemaBracketIdentifier(runes []rune, index int) (schemaToken, int) {
	index++
	start := index
	for index < len(runes) && runes[index] != ']' {
		index++
	}
	token := schemaToken{kind: schemaIdentifierToken, text: strings.ToLower(string(runes[start:index]))}
	if index < len(runes) {
		index++
	}
	return token, index
}

func scanSchemaBareToken(runes []rune, index int) (schemaToken, int) {
	start := index
	for index < len(runes) && isSchemaTokenRune(runes[index]) {
		index++
	}
	return schemaToken{kind: schemaBareToken, text: strings.ToLower(string(runes[start:index]))}, index
}

func isSchemaTokenRune(value rune) bool {
	return value == '_' || value == '$' || unicode.IsLetter(value) || unicode.IsDigit(value)
}

func verifyCurrentOmakitenSchema(ctx context.Context, db schemaQueryer) error {
	expected, err := currentSchemaFingerprint(ctx)
	if err != nil {
		return maintenanceValidationError("current schema baseline is unavailable")
	}
	actual, err := schemaFingerprint(ctx, db)
	if err != nil {
		return maintenanceValidationError("database schema cannot be inspected")
	}
	if actual != expected {
		return maintenanceValidationError("database schema does not match the current baseline; use a new database path or restore a current-compatible backup")
	}
	return nil
}

func releaseSchemaSemanticFingerprint(ctx context.Context) (string, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return "", err
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE document_metadata"); err != nil {
		return "", err
	}
	return semanticSchemaFingerprint(ctx, db)
}
