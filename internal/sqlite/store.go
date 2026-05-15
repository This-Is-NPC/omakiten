package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/migrations"
)

// kitBusyTimeoutMs reads PRAGMA busy_timeout from the embedded kit YAML.
// Used as the fallback when Open is called without a bundle (tests, the
// brief bootstrap window before ConfigService.Import runs). Production
// passes the user's value via OpenWithOptions and never hits this path.
func kitBusyTimeoutMs() int {
	cfg, err := config.LoadKitConfig()
	if err != nil {
		// Embedded YAML failure means the binary is corrupt; the rest of
		// the runtime would also panic. Use a tiny safe value so the
		// caller's error message points at the real failure (the next
		// migration / query) rather than at an opaque PRAGMA reject.
		return 1
	}
	return cfg.SQLite.BusyTimeoutMs
}

// Store wraps the SQLite connection pool with the domain-specific methods used
// by the rest of the app. The methods themselves live in topic-focused files
// (tasks.go, comments.go, bundles.go, ...) so this file stays small and
// focused on lifecycle: opening, closing, and bringing the schema up to date.
//
// Knobs that flow from config (activity_log retention, events fallback) live
// as fields here so the composition root can write them once with
// `SetActivityLogRetention` / `SetEventsRecentLimit` after `Open`. The Store
// has no in-code defaults: zero values mean "config not yet wired" and the
// affected code paths skip work or error out rather than masking the gap.
type Store struct {
	db *sql.DB

	activityLogMaxRows    int
	activityLogMaxAgeDays int
	eventsDefaultRecentLimit int
	// eventsPolicy gates the per-event-type log channel: when
	// ResolveLog returns false, the audit event is dropped before
	// reaching the events table. The zero value resolves to "log
	// everything" so tests that do not wire the policy keep their
	// existing emission assertions.
	eventsPolicy config.EventsSettings
	// bus carries domain events to in-process subscribers (hooks
	// engine, future notifications, future TUI live views). nil disables
	// broadcast — production wires it from composition root, tests
	// inherit a nil bus and silently skip the fan-out.
	bus events.Bus
	// providers is the in-memory config snapshot the Store delegates
	// every read-side config query to. Phase 2 of the config refactor
	// dropped the SQL tables that previously backed these reads
	// (migration 020); the Store now serves workflow shape, personas,
	// skills, laws, templates, notifications, and mcp_commands by
	// reading the active snapshot. Lazily created on first ImportBundle
	// or via Providers() so tests that never call ImportBundle still
	// observe a non-nil (empty-bundle) provider set.
	//
	// TRANSITIONAL (Phase 2-bis / task #117): the Store-side providers
	// are scheduled for deletion. Snapshot() returns a value-typed view
	// of the same bundle; subsequent commits in the chain wire app
	// services and the agent layer to consume Snapshot directly, then
	// drop this field together with bundles.go / personas.go /
	// skills.go / laws.go and the workflow lookup helpers.
	providers *config.InMemoryProviders
	// previousProviders captures the snapshot active immediately before
	// the most recent ImportBundle. OrphanService uses it to resolve the
	// `from_bucket_key` for orphaned tasks — the bucket_id stored on
	// tasks references an id that no longer exists in the new bundle,
	// but the previous snapshot still knows what key that id mapped to.
	// nil until the second ImportBundle of the Store's lifetime.
	previousProviders *config.InMemoryProviders
	// snapshot is the value-typed mirror of providers' active bundle,
	// served via Snapshot() to app services that already consume the
	// Phase 2-bis Snapshot type. ImportBundle rebuilds both providers
	// and snapshot from the same Bundle so the two views stay
	// coherent. previousSnapshot mirrors previousProviders for the
	// orphan flow.
	snapshot         *config.Snapshot
	previousSnapshot *config.Snapshot
}

// Providers returns the in-memory provider set this Store delegates
// config reads to. The pointer is stable for the Store's lifetime;
// callers that want to seed a different snapshot use ImportBundle.
func (s *Store) Providers() *config.InMemoryProviders {
	if s.providers == nil {
		s.providers = config.NewInMemoryProviders(config.Bundle{})
	}
	return s.providers
}

// Snapshot returns the value-typed Phase 2-bis Snapshot mirroring the
// Store's active bundle. Lazily seeded with an empty Snapshot so app
// services (workflow / context / persona / …) that consume the type at
// construction still get a non-nil pointer in tests that skip
// ImportBundle. The pointer is stable until the next ImportBundle
// (which rotates both providers and snapshot in one call).
//
// TRANSITIONAL: this method lives on the Store only until app services
// stop relying on the Store as the snapshot source. Subsequent commits
// in the chain wire agentruntime.ProjectRuntime as the per-project
// origin and delete this accessor together with the legacy providers
// fields.
func (s *Store) Snapshot() *config.Snapshot {
	if s.snapshot == nil {
		s.snapshot = config.BuildSnapshot(config.Bundle{})
	}
	return s.snapshot
}

// PreviousSnapshot returns the value-typed mirror of the bundle active
// immediately before the most recent ImportBundle. Used by the orphan
// flow to resolve task.bucket_id → previous key while the active
// snapshot already rotated. nil until the second ImportBundle.
func (s *Store) PreviousSnapshot() *config.Snapshot {
	return s.previousSnapshot
}

// SetActivityLogRetention installs the operation-log retention window the
// Store applies after every BeginActivityLog. Composition root resolves the
// values from config.activity_log and calls this exactly once at startup.
func (s *Store) SetActivityLogRetention(maxRows, maxAgeDays int) {
	s.activityLogMaxRows = maxRows
	s.activityLogMaxAgeDays = maxAgeDays
}

// SetEventsRecentLimit installs the fallback row count Store.ListRecentEvents
// applies when callers pass <=0. Composition root resolves the value from
// config.events.default_recent_limit.
func (s *Store) SetEventsRecentLimit(limit int) {
	s.eventsDefaultRecentLimit = limit
}

// SetEventsPolicy installs the per-event-type channel policy. When the
// policy resolves Log=false for an event_type, RecordTaskEvent /
// RecordEntityEvent / insertTaskEvent drop the row before insertion
// without surfacing an error to callers.
func (s *Store) SetEventsPolicy(policy config.EventsSettings) {
	s.eventsPolicy = policy
}

// shouldLogEvent reports whether an event of eventType should be
// persisted. Centralised so every emission path consults the same
// resolution logic.
func (s *Store) shouldLogEvent(eventType string) bool {
	return s.eventsPolicy.ResolveLog(eventType)
}

// SetEventBus installs the in-process bus the Store fans events out to
// after every successful emit (post-commit for transactional helpers).
// nil disables broadcast — tests that do not wire a bus inherit the
// existing single-writer semantics.
func (s *Store) SetEventBus(bus events.Bus) {
	s.bus = bus
}

// publishEvent fans an emitted event out to the bus. Caller is
// responsible for placing this AFTER any surrounding tx.Commit so
// subscribers never observe rolled-back rows. Telemetry must not break
// business logic — publish errors are swallowed.
func (s *Store) publishEvent(ctx context.Context, ev domain.Event) {
	if s.bus == nil || ev.EventType == "" {
		return
	}
	_ = s.bus.Publish(ctx, ev)
}

// ConfigKnobs is the resolved bundle of Store-level knobs the composition
// root applies after Open + ConfigService.Import. Wraps the per-area
// setters so the runtime writes them in one place; tests that don't care
// about post-Open re-application skip this entirely and inherit the
// kit-canonical busy_timeout that Open applied.
type ConfigKnobs struct {
	BusyTimeoutMs            int
	ActivityLogMaxRows       int
	ActivityLogMaxAgeDays    int
	EventsDefaultRecentLimit int
	// EventsPolicy mirrors bundle.Config.Events so the Store can apply
	// per-event-type log gates as soon as the bundle reaches it.
	EventsPolicy config.EventsSettings
	// EventBus is the in-process bus the Store fans emitted events to
	// post-commit. nil disables broadcast.
	EventBus events.Bus
}

// ApplyConfig writes the resolved config knobs into the live Store. The
// busy_timeout PRAGMA fires on the borrowed connection — modernc.org/sqlite
// keeps it sticky for the connection's lifetime, and the small pool
// (MaxOpenConns=4) means subsequent connections rerun PRAGMAs at first
// use elsewhere. activity_log + events knobs are simple field writes the
// hot-path code reads without taking a lock.
func (s *Store) ApplyConfig(ctx context.Context, k ConfigKnobs) error {
	if k.BusyTimeoutMs > 0 {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", k.BusyTimeoutMs)); err != nil {
			return fmt.Errorf("apply busy_timeout: %w", err)
		}
	}
	s.SetActivityLogRetention(k.ActivityLogMaxRows, k.ActivityLogMaxAgeDays)
	s.SetEventsRecentLimit(k.EventsDefaultRecentLimit)
	s.SetEventsPolicy(k.EventsPolicy)
	if k.EventBus != nil {
		s.SetEventBus(k.EventBus)
	}
	return nil
}

// Options carries the per-Open knobs that flow from config. Today only
// BusyTimeoutMs is exposed (PRAGMA busy_timeout). Other PRAGMAs
// (foreign_keys, journal_mode, synchronous) describe correctness
// invariants Omakiten depends on and intentionally stay in code.
//
// BusyTimeoutMs == 0 means "use the kit canonical value via Open's
// fallback path"; production passes the value resolved from
// config.sqlite.busy_timeout_ms. Tests that don't load config pass 0
// and inherit the same canonical value (read from the embedded kit
// YAML on first call) so they don't have to thread the bundle around.
type Options struct {
	BusyTimeoutMs int
}

// Open with the kit's default busy_timeout. Reserved for tests and
// composition-root paths that haven't loaded the bundle yet (the
// composition root then re-applies the configured value via the
// store's per-connection pragma helpers when needed).
func Open(ctx context.Context, path string) (*Store, error) {
	return OpenWithOptions(ctx, path, Options{})
}

// OpenWithOptions is the production entry point — composition root
// passes Options.BusyTimeoutMs from the loaded bundle. Zero falls
// back to the kit canonical so test paths don't have to load YAML
// just to open a Store.
func OpenWithOptions(ctx context.Context, path string, opts Options) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// SQLite is single-writer regardless of pool size, so a tiny pool with a
	// single live connection avoids "database is locked" surprises when both
	// the TUI and the MCP server share one Store. Idle conn caps at 2 so the
	// reader pool can warm up without holding extra fds open indefinitely.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	store := &Store{db: db}
	busyTimeout := opts.BusyTimeoutMs
	if busyTimeout <= 0 {
		// Test paths and the bootstrap window (between sqlite.Open and
		// ConfigService.Import) inherit the kit canonical so the engine
		// never runs without a busy_timeout configured.
		busyTimeout = kitBusyTimeoutMs()
	}
	// PRAGMAs run per-connection in SQLite, so they have to fire on every conn
	// the pool hands out — not just once at Open. journal_mode=WAL is the
	// outlier (it persists to the database header), but setting it here is
	// still required so the FIRST connection is the one that flips it.
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL", // WAL-safe; full-fsync is overkill for a local CLI.
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout),
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	if err := store.applyMigrations(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) applyMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)"); err != nil {
		return err
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		var exists int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM schema_migrations WHERE version = ?", name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		data, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}


// placeholders builds an "?,?,?"-shaped string for IN clauses. Lives at the
// package root because tasks.go, comments.go and personas.go all need it for
// parameterised IN-list queries — keeping it here avoids three copies.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
