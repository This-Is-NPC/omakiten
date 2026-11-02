package sqlite

import (
	"context"
	"fmt"
	"time"

	"omakiten/internal/domain"
)

// syncUIDTables are the tables whose rows carry a global uid.
var syncUIDTables = []string{"projects", "tasks", "events", "errors", "solutions", "plans", "plan_waves"}

// seedSyncIdentity inserts the singleton identity row with a fresh device and
// actor.
func seedSyncIdentity(ctx context.Context, db schemaExecutor) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sync_identity (id, device_id, actor_id) VALUES (1, ?, ?)`,
		string(domain.NewUID()), string(domain.NewUID()))
	return err
}

// backfillSyncIdentity gives every existing row a uid and seeds the identity.
func backfillSyncIdentity(ctx context.Context, db schemaExecutor) error {
	for _, table := range syncUIDTables {
		rowids, err := rowidsWithoutUID(ctx, db, table)
		if err != nil {
			return err
		}
		for _, rowid := range rowids {
			if _, err := db.ExecContext(ctx, "UPDATE "+table+" SET uid = ? WHERE rowid = ?", string(domain.NewUID()), rowid); err != nil {
				return fmt.Errorf("set %s uid: %w", table, err)
			}
		}
	}
	return seedSyncIdentity(ctx, db)
}

func rowidsWithoutUID(ctx context.Context, db schemaQueryer, table string) ([]int64, error) {
	rows, err := db.QueryContext(ctx, "SELECT rowid FROM "+table+" WHERE uid IS NULL")
	if err != nil {
		return nil, fmt.Errorf("list %s rows: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var rowids []int64
	for rows.Next() {
		var rowid int64
		if err := rows.Scan(&rowid); err != nil {
			return nil, err
		}
		rowids = append(rowids, rowid)
	}
	return rowids, rows.Err()
}

// SyncIdentity returns the device and actor identities of this database.
func (s *Store) SyncIdentity(ctx context.Context) (device, actor domain.UID, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT device_id, actor_id FROM sync_identity WHERE id = 1`).Scan(&device, &actor)
	return device, actor, err
}

// nextEventHLC ticks the stored hybrid logical clock and persists the new
// stamp in the caller's transaction.
func nextEventHLC(ctx context.Context, exec dbExecutor) (domain.HLC, error) {
	var last, device string
	if err := exec.QueryRowContext(ctx, `SELECT hlc, device_id FROM sync_identity WHERE id = 1`).Scan(&last, &device); err != nil {
		return domain.HLC{}, err
	}
	prev := domain.HLC{}
	if last != "" {
		parsed, err := domain.ParseHLC(last)
		if err != nil {
			return domain.HLC{}, err
		}
		prev = parsed
	}
	next := domain.TickHLC(prev, time.Now().UnixMilli(), domain.UID(device))
	// dbExecutor has no Exec; RETURNING lets the write run through QueryRowContext.
	var stored string
	if err := exec.QueryRowContext(ctx, `UPDATE sync_identity SET hlc = ? WHERE id = 1 RETURNING hlc`, next.String()).Scan(&stored); err != nil {
		return domain.HLC{}, err
	}
	return next, nil
}
