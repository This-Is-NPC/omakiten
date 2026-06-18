package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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

// kitCacheSizeKB mirrors kitBusyTimeoutMs for the cache_size PRAGMA:
// tests + the bootstrap window inherit the embedded kit canonical
// (1024 KiB) so the Store never opens at the SQLite-default 2 MiB
// page cache that the rest of the codebase has long out-grown.
func kitCacheSizeKB() int {
	cfg, err := config.LoadKitConfig()
	if err != nil {
		return 1024
	}
	return cfg.SQLite.CacheSizeKB
}

// Store wraps the SQLite connection pool with the domain-specific methods used
// by the rest of the app. The methods themselves live in topic-focused files
// (tasks.go, comments.go, bundles.go, ...) so this file stays small and
// focused on lifecycle: opening, closing, and bringing the schema up to date.
//
// Knobs that flow from config (events retention, events fallback) live
// as fields here so the composition root can write them once with
// `SetEventsPolicy` / `SetEventsRecentLimit` after `Open`. The Store
// has no in-code defaults: zero values mean "config not yet wired" and the
// affected code paths skip work or error out rather than masking the gap.
type Store struct {
	db *sql.DB

	// busyTimeoutMs is the resolved PRAGMA busy_timeout in milliseconds —
	// the value Open applied to the first pool connection plus any later
	// override committed through ApplyConfig. Per-connection PRAGMAs
	// firing from outside Open's loop (ClaimNextPlanTask reapplies on
	// pinned conns the pool hands out cold) read this field so concurrent
	// callers honour the user's config instead of falling back to the
	// kit default.
	busyTimeoutMs            int
	eventsDefaultRecentLimit int
	retentionGroups          []config.RetentionGroup
	eventTypeRetentionIndex  map[string]int
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

	// versionMu guards the lazily-pinned change-probe connection below.
	versionMu sync.Mutex
	// versionConn is a dedicated connection pinned out of the pool for the
	// Store's lifetime, used exclusively by DataVersion. PRAGMA data_version
	// is per-connection: the counter only advances on a connection when
	// ANOTHER connection (this process's pool or a separate process via the
	// shared WAL) has committed since this connection last read. Reading it
	// through the 2-connection pool (MaxOpenConns=2) would hand back a
	// different physical connection across calls and thrash the counter, so
	// the probe MUST hold one pinned connection. Opened lazily on first
	// DataVersion call and released in Close.
	versionConn *sql.Conn
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
// without surfacing an error to callers. Retention groups are rebuilt
// from the same policy so post-insert pruning uses resolved limits.
func (s *Store) SetEventsPolicy(policy config.EventsSettings) {
	s.eventsPolicy = policy
	s.retentionGroups = policy.BuildRetentionGroups()
	s.eventTypeRetentionIndex = make(map[string]int, len(s.retentionGroups)*4)
	for i, grp := range s.retentionGroups {
		for _, eventType := range grp.EventTypes {
			s.eventTypeRetentionIndex[eventType] = i
		}
	}
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
	BusyTimeoutMs int
	// CacheSizeKB applies PRAGMA cache_size in negative-kilobyte form
	// after Open via ApplyConfig. 0 leaves Open's value in place; <0
	// is rejected by the config validator and never reaches here.
	CacheSizeKB int
	// MmapSizeBytes applies PRAGMA mmap_size. 0 disables mmap; <0 is
	// rejected by the config validator.
	MmapSizeBytes            int
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
// (MaxOpenConns=2) means subsequent connections rerun PRAGMAs at first
// use elsewhere. events policy + recent-limit knobs are simple field
// writes the hot-path code reads without taking a lock.
func (s *Store) ApplyConfig(ctx context.Context, k ConfigKnobs) error {
	if err := applyPragmas(ctx, s.db, pragmaSet{
		BusyTimeoutMs: k.BusyTimeoutMs,
		CacheSizeKB:   k.CacheSizeKB,
		MmapSizeBytes: k.MmapSizeBytes,
	}); err != nil {
		return err
	}
	if k.BusyTimeoutMs > 0 {
		s.busyTimeoutMs = k.BusyTimeoutMs
	}
	s.SetEventsRecentLimit(k.EventsDefaultRecentLimit)
	s.SetEventsPolicy(k.EventsPolicy)
	if k.EventBus != nil {
		s.SetEventBus(k.EventBus)
	}
	return s.pruneAllRetentionGroups(ctx)
}

func (s *Store) pruneAllRetentionGroups(ctx context.Context) error {
	for _, grp := range s.retentionGroups {
		if err := s.PruneEventTypes(ctx, grp.EventTypes, grp.MaxAgeDays, grp.MaxRows); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) pruneRetentionForEventType(ctx context.Context, eventType string) {
	if len(s.retentionGroups) == 0 {
		return
	}
	idx, ok := s.eventTypeRetentionIndex[eventType]
	if !ok {
		return
	}
	grp := s.retentionGroups[idx]
	_ = s.PruneEventTypes(ctx, grp.EventTypes, grp.MaxAgeDays, grp.MaxRows)
}

// pragmaSet carries the user-tunable PRAGMA values applyPragmas issues.
// Correctness PRAGMAs (foreign_keys / journal_mode / synchronous)
// encode engine-level contracts Omakiten depends on, so they stay
// inline in OpenWithOptions and never route through this helper.
//
// "Skip" semantics: BusyTimeoutMs <= 0 and CacheSizeKB <= 0 both skip
// the issue; MmapSizeBytes < 0 skips (0 is a valid value that explicitly
// disables mmap). The Open path fills positive values from the kit
// canonical before calling; the ApplyConfig path passes the raw user-
// supplied knobs and the skip branches preserve the prior per-knob
// optionality.
type pragmaSet struct {
	BusyTimeoutMs int
	CacheSizeKB   int
	MmapSizeBytes int
}

// applyPragmas issues each user-tunable PRAGMA with a uniform
// fmt.Sprintf shape + error wrap. Used by OpenWithOptions's per-
// connection pragma loop AND ApplyConfig's live-write path so a new
// PRAGMA (e.g. temp_store) lands in one file instead of two.
func applyPragmas(ctx context.Context, db *sql.DB, p pragmaSet) error {
	if p.BusyTimeoutMs > 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", p.BusyTimeoutMs)); err != nil {
			return fmt.Errorf("apply busy_timeout: %w", err)
		}
	}
	if p.CacheSizeKB > 0 {
		// cache_size accepts the negative kilobyte form to mean
		// "this many KiB of page cache" regardless of page size.
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA cache_size = -%d", p.CacheSizeKB)); err != nil {
			return fmt.Errorf("apply cache_size: %w", err)
		}
	}
	if p.MmapSizeBytes >= 0 {
		// mmap_size = 0 disables mmap; any positive value asks SQLite
		// to memory-map up to that many bytes of the DB file.
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA mmap_size = %d", p.MmapSizeBytes)); err != nil {
			return fmt.Errorf("apply mmap_size: %w", err)
		}
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
	// CacheSizeKB sets PRAGMA cache_size in negative-kilobyte form.
	// 0 falls back to the kit canonical so test paths inherit it
	// without loading the bundle.
	CacheSizeKB int
	// MmapSizeBytes sets PRAGMA mmap_size; 0 disables mmap (default).
	MmapSizeBytes int
}

// Open with the kit's default busy_timeout. Reserved for tests and
// composition-root paths that haven't loaded the bundle yet (the
// composition root then re-applies the configured value via the
// store's per-connection pragma helpers when needed).
func Open(ctx context.Context, path string) (*Store, error) {
	return OpenWithOptions(ctx, path, Options{})
}

// dsnWithPragmas appends modernc.org/sqlite's `_pragma=...` query params to
// a database path so the per-connection PRAGMAs are applied on EVERY
// connection the driver opens (modernc runs `_pragma` directives in its
// per-connection newConn path), not just whichever pooled connection
// happened to run a one-shot `db.ExecContext` at Open.
//
// foreign_keys, busy_timeout and synchronous are all per-connection in
// SQLite: a single ExecContext at Open lands on one pooled connection,
// leaving a cold second connection at the engine defaults (foreign_keys
// OFF, busy_timeout=0, synchronous=FULL). Threading them through the DSN
// fixes that. cache_size and mmap_size are likewise per-connection (page
// cache / memory-map are owned by each connection), so they ride along
// too — one place, applied uniformly.
//
// modernc accepts multiple `_pragma` params and applies them in order;
// `synchronous(1)` == NORMAL. Works for `:memory:` and bare file paths
// alike — modernc parses the query for pragmas and, for non-`file:`
// DSNs, strips it before handing the path to SQLite. Filesystem paths do
// not contain `?`, so the FIRST param uses a `?` separator; every
// subsequent param uses `&` (the multi-param case the older single-param
// FK builder never exercised). cacheSizeKB / mmapSizeBytes follow the
// same skip semantics as applyPragmas: cacheSizeKB <= 0 and
// mmapSizeBytes < 0 omit the param (0 mmap_size explicitly disables mmap
// and IS emitted).
func dsnWithPragmas(path string, busyTimeoutMs, cacheSizeKB, mmapSizeBytes int) string {
	params := []string{
		"_pragma=foreign_keys(1)",
		"_pragma=synchronous(1)", // 1 == NORMAL; WAL-safe, full-fsync overkill for a local CLI.
	}
	if busyTimeoutMs > 0 {
		params = append(params, fmt.Sprintf("_pragma=busy_timeout(%d)", busyTimeoutMs))
	}
	if cacheSizeKB > 0 {
		// Negative kilobyte form: "this many KiB of page cache".
		params = append(params, fmt.Sprintf("_pragma=cache_size(-%d)", cacheSizeKB))
	}
	if mmapSizeBytes >= 0 {
		params = append(params, fmt.Sprintf("_pragma=mmap_size(%d)", mmapSizeBytes))
	}

	dsn := path
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for _, p := range params {
		dsn += sep + p
		sep = "&"
	}
	return dsn
}

// OpenWithOptions is the production entry point — composition root
// passes Options.BusyTimeoutMs from the loaded bundle. Zero falls
// back to the kit canonical so test paths don't have to load YAML
// just to open a Store.
func OpenWithOptions(ctx context.Context, path string, opts Options) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	// Per-connection PRAGMAs (foreign_keys, busy_timeout, synchronous,
	// cache_size, mmap_size) MUST be threaded through the DSN's `_pragma`
	// query param: SQLite applies them per-connection, so a single
	// `db.ExecContext` after Open only protects whichever pooled
	// connection ran it. A cold second connection (MaxOpenConns=2) would
	// otherwise sit at the engine defaults — foreign_keys OFF (silent
	// no-op FK cascades), busy_timeout=0 (instant SQLITE_BUSY instead of
	// waiting), synchronous=FULL. Threading them through the DSN makes
	// the driver run them on EVERY connection it opens (modernc applies
	// `_pragma` in newConn, after each connect). The query is parsed for
	// pragmas and stripped from the sqlite path for non-URI DSNs
	// (`:memory:` and bare file paths alike), so this is safe for every
	// path the store opens. journal_mode=WAL is the outlier — it persists
	// to the DB header, so it stays a one-shot ExecContext below.
	busyTimeout := opts.BusyTimeoutMs
	if busyTimeout <= 0 {
		// Test paths and the bootstrap window (between sqlite.Open and
		// ConfigService.Import) inherit the kit canonical so the engine
		// never runs without a busy_timeout configured.
		busyTimeout = kitBusyTimeoutMs()
	}
	cacheSize := opts.CacheSizeKB
	if cacheSize <= 0 {
		cacheSize = kitCacheSizeKB()
	}
	mmapSize := opts.MmapSizeBytes
	if mmapSize < 0 {
		mmapSize = 0
	}
	db, err := sql.Open("sqlite", dsnWithPragmas(path, busyTimeout, cacheSize, mmapSize))
	if err != nil {
		return nil, err
	}

	// SQLite is single-writer regardless of pool size, so a tiny pool with a
	// single live connection avoids "database is locked" surprises when both
	// the TUI and the MCP server share one Store. Idle conn caps at 2 so the
	// reader pool can warm up without holding extra fds open indefinitely.
	// MaxOpenConns was originally lowered from 4 → 2 because the TUI is
	// read-mostly and the extra connections never carried real concurrency
	// (single writer regardless) while costing extra fd / cache duplication.
	//
	// It is now 3, not 2: DataVersion pins ONE connection out of the pool for
	// the Store's lifetime (see the versionConn field comment). That pin is
	// permanent, so with MaxOpenConns=2 only a single connection remained for
	// everything else — and the same *Store is shared with the MCP server, so
	// under concurrent TUI + MCP load that lone connection serialized all other
	// work. Reserving 1 for the lifetime data_version pin and keeping 2 usable
	// restores the pre-pin concurrency budget.
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(2)

	store := &Store{db: db}
	// busyTimeout was resolved above (kit canonical fallback) and threaded
	// into the DSN; record it so the per-connection PRAGMA reappliers
	// outside Open's path (ClaimNextPlanTask) honour the same value.
	store.busyTimeoutMs = busyTimeout
	// journal_mode=WAL persists to the DB header (not per-connection), so
	// a single ExecContext on the first connection is enough — and it
	// MUST run once at Open so the header flips before any writer commits.
	// The per-connection PRAGMAs (foreign_keys, busy_timeout, synchronous,
	// cache_size, mmap_size) ride the DSN's _pragma params instead (see
	// dsnWithPragmas above).
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("apply PRAGMA journal_mode = WAL: %w", err)
	}
	if err := store.applyMigrations(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	s.versionMu.Lock()
	if s.versionConn != nil {
		// Best-effort: hand the pinned probe connection back to the pool
		// before the pool itself closes. A Close error here would only mask
		// the db.Close result below, and the OS reclaims the fd regardless.
		_ = s.versionConn.Close()
		s.versionConn = nil
	}
	s.versionMu.Unlock()
	return s.db.Close()
}

// DataVersion returns the SQLite `PRAGMA data_version` watermark read on a
// dedicated connection pinned out of the pool for the Store's lifetime. The
// value is opaque and monotonic-per-connection: it changes whenever any OTHER
// connection — this process's pool OR a separate process sharing the WAL —
// commits a transaction since the pinned connection last read it. It does NOT
// advance for writes committed on the pinned connection itself, but the probe
// connection is read-only, so in practice every external commit (pool writes
// and cross-process writes alike) moves it.
//
// Callers compare successive return values: an unchanged watermark means no
// external write landed and an expensive reload can be skipped; a changed
// watermark means the read model is stale. The pin is mandatory — see the
// versionConn field comment for why a pooled read would thrash.
func (s *Store) DataVersion(ctx context.Context) (int64, error) {
	s.versionMu.Lock()
	defer s.versionMu.Unlock()

	if s.versionConn == nil {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return 0, fmt.Errorf("pin data_version probe connection: %w", err)
		}
		s.versionConn = conn
	}

	var version int64
	if err := s.versionConn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&version); err != nil {
		// Self-heal: a probe error (driver.ErrBadConn, a closed connection, a
		// cancelled ctx that poisoned the conn) leaves versionConn unusable.
		// Close and nil it under the held versionMu so the NEXT call re-pins a
		// fresh connection instead of replaying the broken one forever. Without
		// this the realtime-tick gate would wedge permanently — every later
		// probe reuses the dead conn, the gate falls back to always-reload, and
		// the status line spams the error until process restart. Close runs
		// exactly once before the nil, so there is no double-close, and the
		// re-pin is serialized by the mutex this method already holds.
		_ = s.versionConn.Close()
		s.versionConn = nil
		return 0, fmt.Errorf("read PRAGMA data_version: %w", err)
	}
	return version, nil
}

// Checkpoint forces every committed WAL frame to land in the main
// database file via `PRAGMA wal_checkpoint(TRUNCATE)`. Callers that
// snapshot the .db file (BackupService) invoke this before the copy
// so the snapshot reflects every committed transaction this process
// wrote — without the checkpoint the WAL sidecar carries the latest
// rows and the .db copy misses them.
//
// TRUNCATE mode merges the WAL into the main file and resets the WAL
// to size zero. Returns the underlying SQLite error untouched so the
// caller can decide how to react; the destructive flows treat
// checkpoint failure as best-effort (logged via auditWarn) rather
// than abort because the snapshot still captures the on-disk state.
//
// Cross-process WAL frames written by another `okt` process holding a
// connection to the same DB cannot be guaranteed to land — SQLite may
// return SQLITE_BUSY when foreign writers are active. The contract
// here is "every commit from THIS process lands in main"; concurrent
// writers from another process remain a best-effort case.
func (s *Store) Checkpoint(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	return nil
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

	if err := s.warnTaskDepthBackfillTruncation(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Store) warnTaskDepthBackfillTruncation(ctx context.Context) error {
	var depthColumns int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name = 'depth'`).Scan(&depthColumns); err != nil {
		return err
	}
	if depthColumns == 0 {
		return nil
	}
	var truncatedRows int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE parent_id IS NOT NULL AND depth = 0`).Scan(&truncatedRows); err != nil {
		return err
	}
	if truncatedRows > 0 {
		slog.Warn("tasks depth backfill truncated; descendants > 64 retain depth=0",
			"truncated_rows", truncatedRows,
			"depth_cap", orphanDepthLimit,
		)
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

// boolToInt projects a Go bool into the 0/1 form SQLite expects for
// CASE-WHEN bind parameters. Inline `if b { 1 } else { 0 }` literals are
// short but appear in several writer paths (completed_at gating, future
// plan/assignment toggles) — the helper keeps the call-sites readable
// and the conversion in one place.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
