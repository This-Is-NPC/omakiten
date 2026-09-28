package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
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
		// schema / query) rather than at an opaque PRAGMA reject.
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
// focused on lifecycle: opening, closing, and validating the current schema.
//
// Knobs that flow from config (events retention, events fallback) live
// as fields here so the composition root can write them once with
// `SetEventsPolicy` / `SetEventsRecentLimit` after `Open`. The Store
// has no in-code defaults: zero values mean "config not yet wired" and the
// affected code paths skip work or error out rather than masking the gap.
type Store struct {
	eventRegistries map[int64]*domain.EventRegistry
	db              *sql.DB
	// configMu makes hot-reload configuration publication and readers of the
	// event policy one Store-wide critical section. Database writes remain
	// SQLite-transactional; this lock only prevents torn in-memory settings.
	configMu sync.RWMutex
	// maintenanceConn pins explicit database-maintenance operations to the
	// physical connection opened and identity-checked by OpenSearchMaintenance.
	// Normal stores leave these fields zero and continue using the pool.
	maintenanceMu       sync.Mutex
	maintenanceConn     *sql.Conn
	maintenancePath     string
	maintenanceIdentity os.FileInfo

	// busyTimeoutMs is the resolved PRAGMA busy_timeout in milliseconds:
	// Open threads the initial value through the DSN for every new
	// connection, and ApplyConfig records any later override. ClaimNextPlanTask
	// reapplies this field on its borrowed connection so hot-reloaded config
	// supersedes the DSN's startup value.
	busyTimeoutMs   int
	projectPolicies map[int64]projectEventPolicy
	// bus carries domain events to in-process subscribers (hooks
	// engine, future notifications, future TUI live views). nil disables
	// broadcast — production wires it from composition root, tests
	// inherit a nil bus and silently skip the fan-out.
	bus events.Bus

	// orphanSweepMu guards the reconciliation policy, its schedule, and
	// the warning sink. orphanSweepRunning is the separate non-blocking
	// guard that keeps at most one pass in flight per process without
	// holding orphanSweepMu for the whole (multi-batch) pass.
	orphanSweepMu      sync.Mutex
	orphanSweepPolicy  config.ResolvedOrphanSweep
	orphanSweepNextDue time.Time
	orphanSweepWarn    io.Writer
	// orphanSweepNow replaces the time source in tests so cadence and the
	// wall-clock cap are deterministic. nil means time.Now.
	orphanSweepNow     func() time.Time
	orphanSweepRunning atomic.Bool

	// versionMu guards the lazily-pinned change-probe connection below.
	versionMu sync.Mutex
	// versionConn is a dedicated connection pinned out of the pool for the
	// Store's lifetime, used exclusively by DataVersion. PRAGMA data_version
	// is per-connection: the counter only advances on a connection when
	// ANOTHER connection (this process's pool or a separate process via the
	// shared WAL) has committed since this connection last read. Reading it
	// through the pool would hand back a different physical connection across
	// calls and thrash the counter, so the probe MUST hold one pinned
	// connection. The pool has three slots; all three serve ordinary work until
	// the first DataVersion call pins one for the Store's lifetime, leaving two
	// ordinary slots. The pin is released in Close.
	versionConn *sql.Conn
}

// SetEventsRecentLimit installs the fallback row count Store.ListRecentEvents
// applies when callers pass <=0. Composition root resolves the value from
// config.events.default_recent_limit.
func (s *Store) SetEventsRecentLimit(limit int) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	policy, _ := s.eventPolicyLocked(0)
	policy.recentLimit = limit
	if s.projectPolicies == nil {
		s.projectPolicies = map[int64]projectEventPolicy{}
	}
	s.projectPolicies[0] = policy
}

// SetEventsPolicy installs the per-event-type channel policy. When the
// policy resolves Log=false for an event_type, RecordTaskEvent /
// RecordEntityEvent / insertTaskEvent drop the row before insertion
// without surfacing an error to callers. Retention groups are rebuilt
// from the same policy so post-insert pruning uses resolved limits, and
// the orphan-sweep policy is armed from the same block.
func (s *Store) SetEventsPolicy(policy config.EventsSettings) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.setEventsPolicyLocked(policy)
}

func (s *Store) setEventsPolicyLocked(policy config.EventsSettings) {
	previous, _ := s.eventPolicyLocked(0)
	s.setProjectEventPolicyLocked(0, policy, previous.recentLimit)
}

func (s *Store) setProjectEventPolicyLocked(projectID int64, settings config.EventsSettings, recentLimit int) {
	if s.projectPolicies == nil {
		s.projectPolicies = map[int64]projectEventPolicy{}
	}
	s.projectPolicies[projectID] = newProjectEventPolicy(settings, recentLimit)
	if s.bus != nil {
		s.bus.SetSettings(projectID, settings)
	}
	if projectID == 0 {
		s.SetOrphanSweepPolicy(settings.ResolveOrphanSweep())
	}
}

// shouldLogEvent reports whether an event of eventType should be
// persisted. Centralised so every emission path consults the same
// resolution logic.
func (s *Store) shouldLogEvent(projectID int64, eventType string) bool {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	policy, _ := s.eventPolicyLocked(projectID)
	return policy.settings.ResolveLog(eventType)
}

func (s *Store) recentEventLimit(projectID int64) int {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	policy, _ := s.eventPolicyLocked(projectID)
	return policy.recentLimit
}

func (s *Store) busyTimeout() int {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.busyTimeoutMs
}

// SetEventBus installs the in-process bus the Store fans events out to
// after every successful emit (post-commit for transactional helpers).
// nil disables broadcast — tests that do not wire a bus inherit the
// existing single-writer semantics.
func (s *Store) SetEventBus(bus events.Bus) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.bus = bus
}

// publishEvent fans an emitted event out to the bus. Caller is
// responsible for placing this AFTER any surrounding tx.Commit so
// subscribers never observe rolled-back rows. Telemetry must not break
// business logic — publish errors are swallowed.
func (s *Store) publishEvent(ctx context.Context, ev domain.Event) {
	if ev.EventType == "" {
		return
	}
	s.configMu.RLock()
	bus := s.bus
	s.configMu.RUnlock()
	if bus != nil {
		_ = bus.Publish(ctx, ev)
	}
}

// ConfigKnobs is the resolved bundle of Store-level knobs the composition
// root applies after Open + ConfigService.Import. Wraps the per-area
// setters so the runtime writes them in one place; tests that don't care
// about post-Open re-application skip this entirely and inherit the
// kit-canonical busy_timeout that Open applied.
type ConfigKnobs struct {
	ProjectID     int64
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

// CurrentConfig returns the Store's in-memory runtime configuration. It is
// intended for diagnostics and transaction tests; callers must not mutate the
// returned EventsPolicy maps.
func (s *Store) CurrentConfig() ConfigKnobs {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	policy, _ := s.eventPolicyLocked(0)
	return ConfigKnobs{
		BusyTimeoutMs:            s.busyTimeoutMs,
		EventsDefaultRecentLimit: policy.recentLimit,
		EventsPolicy:             cloneEventsSettings(policy.settings),
		EventBus:                 s.bus,
	}
}

// ApplyConfig writes the resolved config knobs into the live Store. The
// PRAGMAs apply immediately to the borrowed connection; Open's DSN gives
// every newly opened connection the startup values. The busy-timeout override
// is also retained on Store so ClaimNextPlanTask can reapply the hot-reloaded
// value to its connection. The Store-wide config lock publishes all in-memory
// knobs together with the event bus policy.
func (s *Store) ApplyConfig(ctx context.Context, k ConfigKnobs) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	return s.applyConfigLocked(ctx, k)
}

func (s *Store) applyConfigLocked(ctx context.Context, k ConfigKnobs) error {
	registry, err := config.BuildEventRegistry(k.EventsPolicy)
	if err != nil {
		return err
	}
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
	if s.eventRegistries == nil {
		s.eventRegistries = map[int64]*domain.EventRegistry{}
	}
	s.eventRegistries[k.ProjectID] = registry
	if k.EventBus != nil {
		s.bus = k.EventBus
	}
	s.setProjectEventPolicyLocked(k.ProjectID, k.EventsPolicy, k.EventsDefaultRecentLimit)
	if err := s.pruneAllRetentionGroups(ctx, k.ProjectID); err != nil {
		return err
	}
	// Forced reconciliation pass, only after the rest of ApplyConfig
	// succeeded. It is bounded by the same per-pass caps as the
	// opportunistic path, so a database carrying a large orphan backlog
	// costs the composition root one capped pass, not an unbounded scan.
	// A failure here is swallowed: reconciliation is maintenance, and it
	// must never be the reason a runtime refuses to compose. The pass
	// reschedules itself on the retry cadence and the warning goes to
	// the surface-safe sink.
	if _, err := s.SweepOrphanEvents(ctx); err != nil {
		s.warnOrphanSweep(err)
	}
	return nil
}

func (s *Store) pruneAllRetentionGroups(ctx context.Context, projectID int64) error {
	policy, _ := s.eventPolicyLocked(projectID)
	condition, args := s.policyScopeLocked(projectID).condition("")
	for _, group := range policy.groups {
		if err := s.pruneEventTypes(ctx, group.EventTypes, group.MaxAgeDays, group.MaxRows, condition, args); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) pruneRetentionForEventType(ctx context.Context, projectID int64, eventType string) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	policy, scopeID := s.eventPolicyLocked(projectID)
	if position, ok := policy.index[eventType]; ok {
		group := policy.groups[position]
		condition, args := s.policyScopeLocked(scopeID).condition("")
		_ = s.pruneEventTypes(ctx, group.EventTypes, group.MaxAgeDays, group.MaxRows, condition, args)
	}
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

// OpenSearchMaintenance opens a current Omakiten database for explicit search
// diagnostics without changing the schema or persistent journal
// settings. It rejects symlinks in every existing path component and verifies
// the opened file still has the identity observed before sql.Open.
func OpenSearchMaintenance(ctx context.Context, path string) (*Store, error) {
	return openSearchMaintenance(ctx, path, nil)
}

func openSearchMaintenance(ctx context.Context, path string, afterOpen func()) (*Store, error) {
	absolutePath, before, err := validateMaintenancePath(path)
	if err != nil {
		return nil, err
	}
	busyTimeout := kitBusyTimeoutMs()
	uri := sqliteFileURI(absolutePath, "mode=rw")
	dsn := uri + "&_pragma=foreign_keys(1)" + fmt.Sprintf("&_pragma=busy_timeout(%d)", busyTimeout)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, maintenanceValidationError("database could not be opened")
	}
	store := &Store{db: db, busyTimeoutMs: busyTimeout, eventRegistries: defaultEventRegistries()}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(2)
	closeWith := func(err error) (*Store, error) {
		_ = store.Close()
		return nil, err
	}
	maintenanceConn, err := db.Conn(ctx)
	if err != nil {
		return closeWith(maintenanceValidationError("database connection could not be pinned"))
	}
	store.maintenanceConn = maintenanceConn
	store.maintenancePath = absolutePath
	store.maintenanceIdentity = before
	if err := maintenanceConn.PingContext(ctx); err != nil {
		return closeWith(maintenanceValidationError("database could not be opened read-write"))
	}
	if afterOpen != nil {
		afterOpen()
	}
	if err := verifyOpenedDatabaseIdentity(ctx, maintenanceConn, absolutePath, before); err != nil {
		return closeWith(err)
	}
	if err := verifyCurrentOmakitenSchema(ctx, maintenanceConn); err != nil {
		return closeWith(err)
	}
	return store, nil
}

func verifyOpenedDatabaseIdentity(ctx context.Context, db schemaQueryer, path string, before os.FileInfo) error {
	_, after, err := validateMaintenancePath(path)
	if err != nil || !os.SameFile(before, after) {
		return maintenanceValidationError("database file changed while opening")
	}
	var selectedPath string
	if err := db.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&selectedPath); err != nil {
		return maintenanceValidationError("database identity could not be verified")
	}
	_, selected, err := validateMaintenancePath(selectedPath)
	if err != nil || !os.SameFile(before, selected) {
		return maintenanceValidationError("opened database identity does not match requested file")
	}
	return nil
}

func validateMaintenancePath(path string) (string, os.FileInfo, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, maintenanceValidationError("database path is invalid")
	}
	current := filepath.Clean(absolutePath)
	components := []string{current}
	for {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		components = append(components, parent)
		current = parent
	}
	var leaf os.FileInfo
	for index := len(components) - 1; index >= 0; index-- {
		info, err := os.Lstat(components[index])
		if err != nil {
			return "", nil, maintenanceValidationError("database path is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, maintenanceValidationError("database path contains a symlink component")
		}
		if index == 0 {
			leaf = info
		}
	}
	if leaf == nil || !leaf.Mode().IsRegular() {
		return "", nil, maintenanceValidationError("database path must be a regular file")
	}
	return absolutePath, leaf, nil
}

func maintenanceValidationError(reason string) error {
	return domain.NewError(domain.ErrValidation, "database is not a current compatible Omakiten database", map[string]any{
		"reason": reason,
	})
}

type schemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
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
	return openWithOptions(ctx, path, opts, nil)
}

func openWithOptions(ctx context.Context, path string, opts Options, afterOpen func()) (*Store, error) {
	absolutePath, fresh, identity, err := prepareDatabasePath(path)
	if err != nil {
		return nil, err
	}

	// Per-connection PRAGMAs (foreign_keys, busy_timeout, synchronous,
	// cache_size, mmap_size) MUST be threaded through the DSN's `_pragma`
	// query param: SQLite applies them per-connection, so a single
	// `db.ExecContext` after Open only protects whichever pooled
	// connection ran it. A connection opened later would otherwise sit at
	// the engine defaults — foreign_keys OFF (silent
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
	db, err := openSQLiteDatabase(absolutePath, fresh, busyTimeout, cacheSize, mmapSize)
	if err != nil {
		return nil, err
	}
	if afterOpen != nil {
		afterOpen()
	}

	// SQLite writes are serialized. Two ordinary connections remain available
	// while the pinned recovery connection occupies the third pool slot.
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(2)

	store := &Store{db: db, eventRegistries: defaultEventRegistries()}
	// busyTimeout was resolved above (kit canonical fallback) and threaded
	// into the DSN; record it so the per-connection PRAGMA reappliers
	// outside Open's path (ClaimNextPlanTask) honour the same value.
	store.busyTimeoutMs = busyTimeout
	if err := db.PingContext(ctx); err != nil {
		_ = store.Close()
		return nil, maintenanceValidationError("database could not be opened; use a new database path or restore a current-compatible backup")
	}
	if identity != nil {
		if err := verifyOpenedDatabaseIdentity(ctx, db, absolutePath, identity); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	if err := initializeOrValidateDatabase(ctx, db, fresh); err != nil {
		_ = store.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("apply PRAGMA journal_mode = WAL: %w", err)
	}

	return store, nil
}

func openSQLiteDatabase(path string, fresh bool, busyTimeout, cacheSize, mmapSize int) (*sql.DB, error) {
	dsnPath := path
	if !fresh && path != ":memory:" {
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return nil, maintenanceValidationError("database path is invalid")
		}
		dsnPath = sqliteFileURI(absolutePath, "mode=rw")
	}
	db, err := sql.Open("sqlite", dsnWithPragmas(dsnPath, busyTimeout, cacheSize, mmapSize))
	if err != nil {
		return nil, err
	}
	return db, nil
}

func initializeOrValidateDatabase(ctx context.Context, db *sql.DB, fresh bool) error {
	if fresh {
		return applyCurrentSchema(ctx, db)
	}
	if err := verifyCurrentOmakitenSchema(ctx, db); err == nil {
		return nil
	}
	return bridgeV030ReleaseDatabase(ctx, db)
}

func prepareDatabasePath(path string) (string, bool, os.FileInfo, error) {
	if path == ":memory:" {
		return path, true, nil, nil
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", false, nil, maintenanceValidationError("database path is invalid")
	}
	if err := ensureDatabaseParentPath(filepath.Dir(absolutePath)); err != nil {
		return "", false, nil, err
	}
	fresh, err := claimFreshDatabasePath(absolutePath)
	if err != nil {
		return "", false, nil, err
	}
	absolutePath, identity, err := validateMaintenancePath(absolutePath)
	if err != nil {
		return "", false, nil, err
	}
	return absolutePath, fresh, identity, nil
}

func ensureDatabaseParentPath(path string) error {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return maintenanceValidationError("database path is invalid")
	}
	components := []string{filepath.Clean(absolutePath)}
	for current := components[0]; ; {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		components = append(components, parent)
		current = parent
	}
	for index := len(components) - 1; index >= 0; index-- {
		component := components[index]
		info, err := os.Lstat(component)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(component, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return maintenanceValidationError("database path is unavailable")
			}
			info, err = os.Lstat(component)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return maintenanceValidationError("database path contains a symlink or non-directory component")
		}
	}
	return nil
}

func claimFreshDatabasePath(path string) (bool, error) {
	if path == ":memory:" {
		return true, nil
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if err := file.Close(); err != nil {
			return false, err
		}
		return true, nil
	}
	if os.IsExist(err) {
		return false, nil
	}
	return false, err
}

func (s *Store) Close() error {
	s.maintenanceMu.Lock()
	if s.maintenanceConn != nil {
		_ = s.maintenanceConn.Close()
		s.maintenanceConn = nil
	}
	s.maintenanceMu.Unlock()
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

func cloneEventsSettings(in config.EventsSettings) config.EventsSettings {
	out := in
	out.Retention.ByCategory = cloneRetentionSettings(out.Retention.ByCategory)
	out.Retention.Overrides = cloneRetentionSettings(out.Retention.Overrides)
	out.Overrides = cloneEventChannels(out.Overrides)
	out.Definitions = cloneEventDefinitions(out.Definitions)
	return out
}

func cloneEventChannels(in map[string]config.EventChannelSettings) map[string]config.EventChannelSettings {
	if in == nil {
		return nil
	}
	out := make(map[string]config.EventChannelSettings, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneEventDefinitions(in map[string]config.EventDefinitionSettings) map[string]config.EventDefinitionSettings {
	if in == nil {
		return nil
	}
	out := make(map[string]config.EventDefinitionSettings, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneRetentionSettings(in map[string]config.EventRetentionSettings) map[string]config.EventRetentionSettings {
	if in == nil {
		return nil
	}
	out := make(map[string]config.EventRetentionSettings, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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

// Checkpoint forces every committed WAL frame to land in the main database
// file via `PRAGMA wal_checkpoint(TRUNCATE)`. It remains available for legacy
// callers using a generic file-copy snapshot writer; SQLite-aware snapshots use
// VACUUM INTO and include committed WAL frames without checkpointing.
//
// TRUNCATE mode merges the WAL into the main file and resets the WAL
// to size zero. Returns the underlying SQLite error untouched so the
// caller can decide how to react; the destructive flows treat
// checkpoint failure as best-effort (logged via auditWarn).
//
// Cross-process WAL frames written by another `okt` process holding a
// connection to the same DB cannot be guaranteed to land — SQLite may
// return SQLITE_BUSY when foreign writers are active. The contract
// here is "every commit from THIS process lands in main"; concurrent
// writers from another process remain a best-effort case.
func (s *Store) Checkpoint(ctx context.Context) error {
	var busy, logged, checkpointed int
	if err := s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logged, &checkpointed); err != nil {
		return fmt.Errorf("wal_checkpoint query: %w", err)
	}
	if busy != 0 || checkpointed < logged {
		return fmt.Errorf("wal_checkpoint incomplete: busy=%d logged=%d checkpointed=%d", busy, logged, checkpointed)
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
