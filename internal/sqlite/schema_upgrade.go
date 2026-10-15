package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// schemaStep is the part of schema.sql a schema version adds. Its statements
// start at marker and run to the next step's marker or the end of the file.
type schemaStep struct {
	version int
	marker  string
}

// schemaSteps lists, in order, the additions after the version 1 baseline.
var schemaSteps = []schemaStep{
	{version: 2, marker: "CREATE TABLE document_metadata"},
	{version: 3, marker: "CREATE TABLE agent_teams"},
}

// schemaThrough returns the statements of the schema at version.
func schemaThrough(version int) string {
	for _, step := range schemaSteps {
		if step.version > version {
			return schemaSQL[:strings.Index(schemaSQL, step.marker)]
		}
	}
	return schemaSQL
}

// stepSQL returns the statements one step adds.
func stepSQL(i int) string {
	start := strings.Index(schemaSQL, schemaSteps[i].marker)
	if i+1 < len(schemaSteps) {
		return schemaSQL[start:strings.Index(schemaSQL, schemaSteps[i+1].marker)]
	}
	return schemaSQL[start:]
}

type schemaExecutor interface {
	schemaQueryer
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// applySchemaSteps adds every step after version and records the current
// schema version.
func applySchemaSteps(ctx context.Context, db schemaExecutor, version int) error {
	for i, step := range schemaSteps {
		if step.version <= version {
			continue
		}
		if _, err := db.ExecContext(ctx, stepSQL(i)); err != nil {
			return fmt.Errorf("add schema version %d: %w", step.version, err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", step.version)); err != nil {
			return fmt.Errorf("set schema version %d: %w", step.version, err)
		}
	}
	return nil
}

// schemaFingerprintAt fingerprints a fresh database at version.
func schemaFingerprintAt(ctx context.Context, version int) (string, error) {
	baseline, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return "", err
	}
	defer func() { _ = baseline.Close() }()
	baseline.SetMaxOpenConns(1)
	if _, err := baseline.ExecContext(ctx, schemaThrough(version)); err != nil {
		return "", err
	}
	if _, err := baseline.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return "", err
	}
	return schemaFingerprint(ctx, baseline)
}

// upgradeSchema brings a database that exactly matches the schema at version
// to the current schema in one transaction.
func upgradeSchema(ctx context.Context, db *sql.DB, version int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	actual, err := schemaFingerprint(ctx, tx)
	if err != nil {
		return err
	}
	expected, err := schemaFingerprintAt(ctx, version)
	if err != nil {
		return err
	}
	if actual != expected {
		return maintenanceValidationError("database schema does not match the supported baseline; use a new database path or restore a current-compatible backup")
	}
	if err := applySchemaSteps(ctx, tx, version); err != nil {
		return err
	}
	if err := verifyCurrentOmakitenSchema(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
