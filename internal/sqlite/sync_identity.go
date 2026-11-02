package sqlite

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"

	"omakiten/internal/domain"
)

// syncUIDTables are the tables whose rows carry a global uid.
var syncUIDTables = []string{"projects", "tasks", "events", "errors", "solutions", "plans", "plan_waves"}

// backfillSyncIdentity gives every existing row a uid with one set-based
// UPDATE per table, then seeds the identity. The triggers of those tables
// (search index, capture) are dropped around the updates and recreated from
// their stored definitions: the uid is no change they have to see, and the
// search index trigger scans its table once per updated row.
func backfillSyncIdentity(ctx context.Context, db schemaExecutor) error {
	triggers, err := triggersOn(ctx, db, syncUIDTables)
	if err != nil {
		return err
	}
	for _, trigger := range triggers {
		if _, err := db.ExecContext(ctx, "DROP TRIGGER "+trigger.name); err != nil {
			return fmt.Errorf("drop trigger %s: %w", trigger.name, err)
		}
	}
	for _, table := range syncUIDTables {
		if _, err := db.ExecContext(ctx, "UPDATE "+table+" SET uid = okt_uuid7() WHERE uid IS NULL"); err != nil {
			return fmt.Errorf("set %s uid: %w", table, err)
		}
	}
	for _, trigger := range triggers {
		if _, err := db.ExecContext(ctx, trigger.sql); err != nil {
			return fmt.Errorf("recreate trigger %s: %w", trigger.name, err)
		}
	}
	return seedSyncIdentity(ctx, db)
}

type storedTrigger struct{ name, sql string }

// triggersOn lists the triggers of tables in creation order.
func triggersOn(ctx context.Context, db schemaQueryer, tables []string) ([]storedTrigger, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, sql FROM sqlite_master WHERE type = 'trigger' AND tbl_name IN ('`+
		strings.Join(tables, "', '")+`') ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var triggers []storedTrigger
	for rows.Next() {
		var trigger storedTrigger
		if err := rows.Scan(&trigger.name, &trigger.sql); err != nil {
			return nil, err
		}
		triggers = append(triggers, trigger)
	}
	return triggers, rows.Err()
}

// seedSyncIdentity inserts the singleton identity row with a fresh device and
// actor.
func seedSyncIdentity(ctx context.Context, db schemaExecutor) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sync_identity (id, device_id, actor_id) VALUES (1, ?, ?)`,
		string(domain.NewUID()), string(domain.NewUID()))
	return err
}

// SyncIdentity returns the device and actor identities of this database.
func (s *Store) SyncIdentity(ctx context.Context) (device, actor domain.UID, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT device_id, actor_id FROM sync_identity WHERE id = 1`).Scan(&device, &actor)
	return device, actor, err
}

// eventStamp is the sync envelope written on every events row.
type eventStamp struct {
	UID    string
	HLC    string
	Device string
	Actor  string
}

// stampEvent ticks the stored hybrid logical clock and returns the envelope
// for one events row. The tick is a write, so the transaction takes the write
// lock before it reads the clock.
func stampEvent(ctx context.Context, exec dbExecutor) (eventStamp, error) {
	var stamp eventStamp
	err := exec.QueryRowContext(ctx,
		`UPDATE sync_identity SET hlc = okt_hlc_tick(hlc, device_id) WHERE id = 1 RETURNING hlc, device_id, actor_id`).
		Scan(&stamp.HLC, &stamp.Device, &stamp.Actor)
	stamp.UID = string(domain.NewUID())
	return stamp, err
}

// tickHLC is the okt_hlc_tick SQL function: the next stamp after last for device.
func tickHLC(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	last, _ := args[0].(string)
	device, _ := args[1].(string)
	prev := domain.HLC{}
	if last != "" {
		parsed, err := domain.ParseHLC(last)
		if err != nil {
			return nil, err
		}
		prev = parsed
	}
	next, err := domain.TickHLC(prev, time.Now().UnixMilli(), domain.UID(device))
	if err != nil {
		return nil, err
	}
	return next.String(), nil
}

// newUID is the okt_uuid7 SQL function: a fresh uid per call.
func newUID(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
	return string(domain.NewUID()), nil
}

// The functions are registered for every connection opened by this process,
// before any schema that names them is used.
func init() {
	sqlite.MustRegisterScalarFunction("okt_hlc_tick", 2, tickHLC)
	sqlite.MustRegisterScalarFunction("okt_uuid7", 0, newUID)
}
