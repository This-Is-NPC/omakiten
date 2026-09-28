package sqlite

import (
	"context"
	"database/sql"
)

// ValidateV030ReleaseDatabase reports whether path is the exact v0.30.0
// database source accepted by the one-time updater transition. SQLite remains
// responsible for normal WAL/SHM coordination; this check only validates the
// regular database file and its schema.
func ValidateV030ReleaseDatabase(ctx context.Context, path string) error {
	absolutePath, identity, err := validateMaintenancePath(path)
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", sqliteFileURI(absolutePath, "mode=ro"))
	if err != nil {
		return maintenanceValidationError("database could not be opened read-only")
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return maintenanceValidationError("database could not be opened read-only")
	}
	if err := verifyOpenedDatabaseIdentity(ctx, db, absolutePath, identity); err != nil {
		return err
	}
	return validateV030ReleaseDatabase(ctx, db)
}

// OpenCurrentReadOnly opens and validates a current database without creating
// path components, changing PRAGMAs, or enabling WAL. It is used for updater
// discovery before the command has established that an update is required.
func OpenCurrentReadOnly(ctx context.Context, path string) (*Store, error) {
	absolutePath, identity, err := validateMaintenancePath(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteFileURI(absolutePath, "mode=ro"))
	if err != nil {
		return nil, maintenanceValidationError("database could not be opened read-only")
	}
	db.SetMaxOpenConns(1)
	closeWith := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return closeWith(maintenanceValidationError("database could not be opened read-only"))
	}
	if err := verifyOpenedDatabaseIdentity(ctx, db, absolutePath, identity); err != nil {
		return closeWith(err)
	}
	if err := verifyCurrentOmakitenSchema(ctx, db); err != nil {
		return closeWith(err)
	}
	return &Store{db: db}, nil
}
