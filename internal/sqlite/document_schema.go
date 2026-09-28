package sqlite

import (
	"context"
	"database/sql"
	"strings"
)

func documentMetadataSQL() string {
	return schemaSQL[strings.Index(schemaSQL, "CREATE TABLE document_metadata"):]
}

func upgradeDocumentSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	actual, err := schemaFingerprint(ctx, tx)
	if err != nil {
		return err
	}
	baseline, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer func() { _ = baseline.Close() }()
	if _, err := baseline.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	if _, err := baseline.ExecContext(ctx, "DROP TABLE document_metadata; PRAGMA user_version = 1"); err != nil {
		return err
	}
	expected, err := schemaFingerprint(ctx, baseline)
	if err != nil {
		return err
	}
	if actual != expected {
		return maintenanceValidationError("database schema does not match the supported baseline; use a new database path or restore a current-compatible backup")
	}
	if _, err := tx.ExecContext(ctx, documentMetadataSQL()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		return err
	}
	if err := verifyCurrentOmakitenSchema(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
