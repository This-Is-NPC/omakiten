package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/operation"
	"omakiten/internal/sqlite"
	"omakiten/internal/testfixtures"
)

const (
	claimBenchmarkSchemaVersion = 1
	claimBenchmarkName          = "mcp.plans.claim_next.agent_ceiling"
)

func TestClaimNextCeilingProtocolDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := claimBenchmarkConfigFromEnv()
	if err != nil {
		t.Fatalf("claimBenchmarkConfigFromEnv() error = %v", err)
	}
	wantLevels := []int{1, 2, 4, 8, 16, 32, 64, 128}
	if !equalInts(cfg.CoarseLevels, wantLevels) {
		t.Fatalf("coarse levels = %v, want %v", cfg.CoarseLevels, wantLevels)
	}
	if cfg.WarmupBursts != 3 || cfg.MeasuredBursts != 30 || cfg.Invocations != 5 || cfg.BoundaryBursts != 100 {
		t.Fatalf("protocol counts = %+v, want warmups=3 measured=30 invocations=5 boundary=100", cfg)
	}
}

func TestClaimNextCeilingResultValidation(t *testing.T) {
	t.Parallel()

	result := claimBenchmarkResult{
		SchemaVersion: claimBenchmarkSchemaVersion,
		Benchmark:     claimBenchmarkName,
		GeneratedAt:   "2026-07-15T00:00:00Z",
		ReproductionCommand: "OKT_CLAIM_BENCH_LEVELS=128 OKT_CLAIM_BENCH_WARMUPS=3 " +
			"OKT_CLAIM_BENCH_BURSTS=30 OKT_CLAIM_BENCH_INVOCATIONS=5 " +
			"OKT_CLAIM_BENCH_BOUNDARY_BURSTS=100 " +
			"OKT_CLAIM_BENCH_OUT=.docs/internal/claim-next-agent-ceiling-reference.json " +
			"go test ./internal/mcp -run '^$' -bench '^BenchmarkClaimNextAgentCeiling$' -benchtime=1x -count=1",
		Protocol: claimBenchmarkProtocol{
			CoarseLevels:                []int{128},
			ConfiguredMaximum:           128,
			WarmupBurstsPerInvocation:   3,
			MeasuredBurstsPerInvocation: 30,
			FreshDatabaseInvocations:    5,
			BoundaryBurstsPerInvocation: 100,
			TasksPerBurst:               "one newly seeded task per caller",
			SynchronizedStart:           true,
			IndependentLongLivedStacks:  true,
			FileBackedSharedWAL:         true,
			CorrectnessOracle:           make([]string, 5),
		},
		Environment: claimBenchmarkEnvironment{
			GOOS:            "linux",
			GOARCH:          "amd64",
			GoVersion:       "go1.25.12",
			LogicalCPUs:     12,
			GOMAXPROCS:      12,
			SourceRevision:  "0123456789abcdef",
			DatabaseStorage: "file-backed database in /tmp",
		},
		SQLite: claimBenchmarkSQLite{
			Version:       "3.53.0",
			Driver:        "modernc.org/sqlite",
			DriverVersion: "v1.50.0",
			Pragmas: map[string]any{
				"journal_mode": "wal", "foreign_keys": 1, "synchronous": 1,
				"busy_timeout": 5000, "cache_size": -1024, "mmap_size": 0, "temp_store": 0,
			},
			Pool: claimBenchmarkPoolInfo{
				StackShape:                 "one sqlite.Store, operation.Service, and mcp.Adapter per caller",
				MaxOpenConnectionsPerStore: 3,
				MaxIdleConnectionsPerStore: 2,
				DataVersionPinRequested:    true,
				PinnedConnectionsPerStore:  1,
				OrdinarySlotsAfterPin:      2,
			},
		},
		Summary: claimBenchmarkSummary{
			Ceiling:                         ">=128",
			LargestContiguousErrorFreeLevel: 128,
			BoundaryLevel:                   128,
			BoundaryPassed:                  true,
		},
		Levels: []claimBenchmarkLevelResult{
			{Phase: "coarse", Concurrency: 128, Invocations: 5, WarmupBursts: 15, MeasuredBursts: 150, QuickChecksPassed: 5, Passed: true},
			{Phase: "boundary", Concurrency: 128, Invocations: 5, WarmupBursts: 15, MeasuredBursts: 500, QuickChecksPassed: 5, Passed: true},
		},
	}
	if err := validateClaimBenchmarkResult(result); err != nil {
		t.Fatalf("validateClaimBenchmarkResult() error = %v", err)
	}

	result.Levels[1].MeasuredBursts = 499
	if err := validateClaimBenchmarkResult(result); err == nil {
		t.Fatal("validateClaimBenchmarkResult() accepted a 499-burst final boundary")
	}

	result.Levels[1].MeasuredBursts = 500
	result.Environment.SourceRevision = ""
	if err := validateClaimBenchmarkResult(result); err == nil {
		t.Fatal("validateClaimBenchmarkResult() accepted missing source revision metadata")
	}
}

func TestClaimNextCeilingReproductionCommand(t *testing.T) {
	t.Parallel()

	cfg := claimBenchmarkConfig{
		CoarseLevels:   []int{1, 2, 4, 8},
		WarmupBursts:   3,
		MeasuredBursts: 30,
		Invocations:    5,
		BoundaryBursts: 100,
	}
	want := "OKT_CLAIM_BENCH_LEVELS=1,2,4,8 OKT_CLAIM_BENCH_WARMUPS=3 " +
		"OKT_CLAIM_BENCH_BURSTS=30 OKT_CLAIM_BENCH_INVOCATIONS=5 " +
		"OKT_CLAIM_BENCH_BOUNDARY_BURSTS=100 " +
		"OKT_CLAIM_BENCH_OUT=.docs/internal/claim-next-agent-ceiling-reference.json " +
		"go test ./internal/mcp -run '^$' -bench '^BenchmarkClaimNextAgentCeiling$' -benchtime=1x -count=1"
	if got := claimBenchmarkReproductionCommand(cfg); got != want {
		t.Fatalf("reproduction command = %q, want %q", got, want)
	}
}

func TestClaimNextCeilingReference(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", ".docs", "internal", "claim-next-agent-ceiling-reference.json"))
	if err != nil {
		t.Fatalf("read reference result: %v", err)
	}
	var result claimBenchmarkResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode reference result: %v", err)
	}
	if err := validateClaimBenchmarkResult(result); err != nil {
		t.Fatalf("validate reference result: %v", err)
	}

	wantRequired := []int{1, 2, 4, 8, 16, 32, 64, 128}
	if len(result.Protocol.CoarseLevels) < len(wantRequired) ||
		!equalInts(result.Protocol.CoarseLevels[:len(wantRequired)], wantRequired) {
		t.Fatalf("reference coarse levels = %v, want required prefix %v", result.Protocol.CoarseLevels, wantRequired)
	}
	if result.Protocol.WarmupBurstsPerInvocation != 3 || result.Protocol.MeasuredBurstsPerInvocation != 30 ||
		result.Protocol.FreshDatabaseInvocations != 5 || result.Protocol.BoundaryBurstsPerInvocation != 100 {
		t.Fatalf("reference protocol counts = %+v, want 3/30/5 and boundary 100", result.Protocol)
	}
	if result.Summary.LargestContiguousErrorFreeLevel < 1 || !result.Summary.BoundaryPassed {
		t.Fatalf("reference has no validated boundary: %+v", result.Summary)
	}
}

func TestClaimNextCeilingReferenceFreshness(t *testing.T) {
	if os.Getenv("OKT_CHECK_CLAIM_BENCH_FRESHNESS") != "1" {
		t.Skip("manual freshness check; run mise run claim-benchmark:freshness")
	}

	path := filepath.Join("..", "..", ".docs", "internal", "claim-next-agent-ceiling-reference.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read reference result: %v", err)
	}
	var result claimBenchmarkResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode reference result: %v", err)
	}
	if err := validateClaimBenchmarkResult(result); err != nil {
		t.Fatalf("validate reference result: %v", err)
	}

	reasons := claimBenchmarkFreshnessReasons(t, result)
	if len(reasons) > 0 {
		t.Fatalf("ClaimNext benchmark reference needs a clean manual rerun:\n- %s", strings.Join(reasons, "\n- "))
	}
}

func claimBenchmarkFreshnessReasons(t *testing.T, result claimBenchmarkResult) []string {
	t.Helper()
	var reasons []string
	if result.Environment.SourceModified {
		reasons = append(reasons, "reference was captured from a dirty worktree (environment.source_modified=true)")
	}
	if result.Environment.GoVersion != runtime.Version() {
		reasons = append(reasons, fmt.Sprintf("Go version changed from %s to %s", result.Environment.GoVersion, runtime.Version()))
	}

	root, err := claimBenchmarkRepoRoot()
	if err != nil {
		return append(reasons, err.Error())
	}
	paths := []string{
		"defaults/omakiten.yaml",
		"go.mod",
		"go.sum",
		"internal/operation/service_plans.go",
		"internal/mcp/adapter.go",
		"internal/mcp/claim_next_ceiling_bench_test.go",
		"internal/sqlite/plans.go",
		"internal/sqlite/store.go",
	}
	args := append([]string{"diff", "--name-only", result.Environment.SourceRevision, "--"}, paths...)
	diff := exec.Command("git", args...)
	diff.Dir = root
	output, err := diff.Output()
	if err != nil {
		reasons = append(reasons, fmt.Sprintf("cannot compare source revision %s: %v", result.Environment.SourceRevision, err))
	} else if changed := strings.Fields(string(output)); len(changed) > 0 {
		reasons = append(reasons, "benchmark inputs changed: "+strings.Join(changed, ", "))
	}

	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	currentSQLite, err := collectClaimBenchmarkSQLite(context.Background(), filepath.Join(t.TempDir(), "freshness.db"), bundle)
	if err != nil {
		reasons = append(reasons, "cannot inspect current SQLite configuration: "+err.Error())
	} else if currentSQLite.Version != result.SQLite.Version ||
		currentSQLite.Driver != result.SQLite.Driver ||
		currentSQLite.DriverVersion != result.SQLite.DriverVersion ||
		currentSQLite.Pool != result.SQLite.Pool ||
		fmt.Sprint(currentSQLite.Pragmas) != fmt.Sprint(result.SQLite.Pragmas) {
		reasons = append(reasons, "SQLite version, driver, PRAGMAs, or connection-pool shape changed")
	}
	return reasons
}

func TestClaimNextCeilingFallsBackAfterBoundaryFailure(t *testing.T) {
	t.Parallel()

	levels := []claimBenchmarkLevelResult{
		{Phase: "coarse", Concurrency: 64, Passed: true},
		{Phase: "refined", Concurrency: 88, Passed: true},
		{Phase: "refined", Concurrency: 90, Passed: true},
		{Phase: "boundary", Concurrency: 90, Passed: false},
	}
	if got := highestPassingClaimBenchmarkLevelBelow(levels, 90); got != 88 {
		t.Fatalf("fallback level = %d, want 88", got)
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestClaimNextCeilingMCPOracle(t *testing.T) {
	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	cfg := claimBenchmarkConfig{
		CoarseLevels:   []int{4},
		WarmupBursts:   1,
		MeasuredBursts: 2,
		Invocations:    1,
		BoundaryBursts: 2,
	}
	level, err := runClaimBenchmarkLevel(context.Background(), t.TempDir(), config.BuildSnapshot(bundle), bundle, cfg, "coarse", 4, 2)
	if err != nil {
		t.Fatalf("runClaimBenchmarkLevel() error = %v", err)
	}
	if !level.Passed {
		t.Fatalf("MCP oracle failed: errors=%+v warmup_errors=%+v samples=%v", level.Errors, level.WarmupErrors, level.ErrorSamples)
	}
	if level.MeasuredBursts != 2 || level.ClaimsAttempted != 8 || level.ClaimsSucceeded != 8 {
		t.Fatalf("measured level = %+v, want 2 bursts and 8/8 claims", level)
	}
	if level.QuickChecksPassed != 1 {
		t.Fatalf("quick checks = %d, want one after the fresh-database invocation", level.QuickChecksPassed)
	}
	if got := level.InvocationResults[0].SeededRowsCleaned; got != 12 {
		t.Fatalf("seeded rows cleaned = %d, want 12 across three 4-caller bursts", got)
	}
}

// BenchmarkClaimNextAgentCeiling is intentionally a protocol runner rather
// than a microbenchmark. Use -benchtime=1x so Go executes exactly one complete
// coarse/refined/boundary measurement and OKT_CLAIM_BENCH_OUT to retain JSON.
func BenchmarkClaimNextAgentCeiling(b *testing.B) {
	if b.N != 1 {
		b.Fatalf("BenchmarkClaimNextAgentCeiling requires -benchtime=1x (b.N=%d)", b.N)
	}
	cfg, err := claimBenchmarkConfigFromEnv()
	if err != nil {
		b.Fatal(err)
	}
	bundle, _ := testfixtures.LoadBundle(b, "default.yaml")

	b.ResetTimer()
	result, err := runClaimBenchmark(context.Background(), b.TempDir(), bundle, cfg)
	b.StopTimer()
	if err != nil {
		b.Fatal(err)
	}
	if err := validateClaimBenchmarkResult(result); err != nil {
		b.Fatalf("validate result: %v", err)
	}
	if cfg.OutputPath != "" {
		path, err := claimBenchmarkOutputPath(cfg.OutputPath)
		if err != nil {
			b.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatalf("create result directory: %v", err)
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			b.Fatalf("marshal result: %v", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(path, data, 0o644); err != nil {
			b.Fatalf("write %s: %v", path, err)
		}
		b.Logf("wrote %s", path)
	}
	b.ReportMetric(float64(result.Summary.LargestContiguousErrorFreeLevel), "error-free-agents")
	b.Logf("ceiling=%s first_failing_level=%v", result.Summary.Ceiling, result.Summary.FirstFailingLevel)
}

type claimBenchmarkConfig struct {
	CoarseLevels   []int
	WarmupBursts   int
	MeasuredBursts int
	Invocations    int
	BoundaryBursts int
	OutputPath     string
}

func claimBenchmarkConfigFromEnv() (claimBenchmarkConfig, error) {
	maxLevel, err := positiveEnv("OKT_CLAIM_BENCH_MAX", 128)
	if err != nil {
		return claimBenchmarkConfig{}, err
	}
	levels := powersOfTwoThrough(maxLevel)
	if raw := strings.TrimSpace(os.Getenv("OKT_CLAIM_BENCH_LEVELS")); raw != "" {
		levels, err = parseClaimBenchmarkLevels(raw)
		if err != nil {
			return claimBenchmarkConfig{}, err
		}
	}
	warmups, err := positiveEnv("OKT_CLAIM_BENCH_WARMUPS", 3)
	if err != nil {
		return claimBenchmarkConfig{}, err
	}
	bursts, err := positiveEnv("OKT_CLAIM_BENCH_BURSTS", 30)
	if err != nil {
		return claimBenchmarkConfig{}, err
	}
	invocations, err := positiveEnv("OKT_CLAIM_BENCH_INVOCATIONS", 5)
	if err != nil {
		return claimBenchmarkConfig{}, err
	}
	boundary, err := positiveEnv("OKT_CLAIM_BENCH_BOUNDARY_BURSTS", 100)
	if err != nil {
		return claimBenchmarkConfig{}, err
	}
	return claimBenchmarkConfig{
		CoarseLevels:   levels,
		WarmupBursts:   warmups,
		MeasuredBursts: bursts,
		Invocations:    invocations,
		BoundaryBursts: boundary,
		OutputPath:     strings.TrimSpace(os.Getenv("OKT_CLAIM_BENCH_OUT")),
	}, nil
}

func claimBenchmarkReproductionCommand(cfg claimBenchmarkConfig) string {
	levels := make([]string, len(cfg.CoarseLevels))
	for i, level := range cfg.CoarseLevels {
		levels[i] = strconv.Itoa(level)
	}
	return fmt.Sprintf(
		"OKT_CLAIM_BENCH_LEVELS=%s OKT_CLAIM_BENCH_WARMUPS=%d OKT_CLAIM_BENCH_BURSTS=%d "+
			"OKT_CLAIM_BENCH_INVOCATIONS=%d OKT_CLAIM_BENCH_BOUNDARY_BURSTS=%d "+
			"OKT_CLAIM_BENCH_OUT=.docs/internal/claim-next-agent-ceiling-reference.json "+
			"go test ./internal/mcp -run '^$' -bench '^BenchmarkClaimNextAgentCeiling$' -benchtime=1x -count=1",
		strings.Join(levels, ","), cfg.WarmupBursts, cfg.MeasuredBursts, cfg.Invocations, cfg.BoundaryBursts,
	)
}

func positiveEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return value, nil
}

func powersOfTwoThrough(maximum int) []int {
	levels := make([]int, 0, 8)
	for level := 1; level <= maximum; level *= 2 {
		levels = append(levels, level)
		if level > maximum/2 {
			break
		}
	}
	return levels
}

func parseClaimBenchmarkLevels(raw string) ([]int, error) {
	seen := make(map[int]struct{})
	levels := make([]int, 0)
	for _, part := range strings.Split(raw, ",") {
		level, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || level <= 0 {
			return nil, fmt.Errorf("OKT_CLAIM_BENCH_LEVELS must contain positive comma-separated integers, got %q", raw)
		}
		if _, exists := seen[level]; exists {
			continue
		}
		seen[level] = struct{}{}
		levels = append(levels, level)
	}
	sort.Ints(levels)
	if len(levels) == 0 {
		return nil, fmt.Errorf("OKT_CLAIM_BENCH_LEVELS must not be empty")
	}
	return levels, nil
}

type claimBenchmarkResult struct {
	SchemaVersion       int                         `json:"schema_version"`
	Benchmark           string                      `json:"benchmark"`
	GeneratedAt         string                      `json:"generated_at"`
	ReproductionCommand string                      `json:"reproduction_command"`
	Protocol            claimBenchmarkProtocol      `json:"protocol"`
	Environment         claimBenchmarkEnvironment   `json:"environment"`
	SQLite              claimBenchmarkSQLite        `json:"sqlite"`
	Summary             claimBenchmarkSummary       `json:"summary"`
	Levels              []claimBenchmarkLevelResult `json:"levels"`
}

type claimBenchmarkProtocol struct {
	CoarseLevels                []int    `json:"coarse_levels"`
	RefinedLevels               []int    `json:"refined_levels"`
	ConfiguredMaximum           int      `json:"configured_maximum"`
	WarmupBurstsPerInvocation   int      `json:"warmup_bursts_per_invocation"`
	MeasuredBurstsPerInvocation int      `json:"measured_bursts_per_invocation"`
	FreshDatabaseInvocations    int      `json:"fresh_database_invocations"`
	BoundaryBurstsPerInvocation int      `json:"boundary_bursts_per_invocation"`
	TasksPerBurst               string   `json:"tasks_per_burst"`
	SynchronizedStart           bool     `json:"synchronized_start"`
	IndependentLongLivedStacks  bool     `json:"independent_long_lived_stacks"`
	FileBackedSharedWAL         bool     `json:"file_backed_shared_wal"`
	LatencyIsPassCriterion      bool     `json:"latency_is_pass_criterion"`
	CorrectnessOracle           []string `json:"correctness_oracle"`
}

type claimBenchmarkEnvironment struct {
	GOOS            string `json:"goos"`
	GOARCH          string `json:"goarch"`
	GoVersion       string `json:"go_version"`
	LogicalCPUs     int    `json:"logical_cpus"`
	GOMAXPROCS      int    `json:"gomaxprocs"`
	CPUModel        string `json:"cpu_model,omitempty"`
	KernelRelease   string `json:"kernel_release,omitempty"`
	MemoryTotalKiB  int64  `json:"memory_total_kib,omitempty"`
	SourceRevision  string `json:"source_revision,omitempty"`
	SourceModified  bool   `json:"source_modified,omitempty"`
	DatabaseStorage string `json:"database_storage"`
}

type claimBenchmarkSQLite struct {
	Version       string                 `json:"version"`
	Driver        string                 `json:"driver"`
	DriverVersion string                 `json:"driver_version"`
	Pragmas       map[string]any         `json:"pragmas"`
	Pool          claimBenchmarkPoolInfo `json:"pool"`
}

type claimBenchmarkPoolInfo struct {
	StackShape                 string `json:"stack_shape"`
	SharedStore                bool   `json:"shared_store"`
	MaxOpenConnectionsPerStore int    `json:"max_open_connections_per_store"`
	MaxIdleConnectionsPerStore int    `json:"max_idle_connections_per_store"`
	DataVersionPinRequested    bool   `json:"data_version_pin_requested"`
	PinnedConnectionsPerStore  int    `json:"pinned_connections_per_store"`
	OrdinarySlotsAfterPin      int    `json:"ordinary_slots_after_pin"`
}

type claimBenchmarkSummary struct {
	Ceiling                         string               `json:"ceiling"`
	LargestContiguousErrorFreeLevel int                  `json:"largest_contiguous_error_free_level"`
	FirstFailingLevel               *int                 `json:"first_failing_level"`
	FirstFailureErrors              claimBenchmarkErrors `json:"first_failure_errors"`
	NoFailureObserved               bool                 `json:"no_failure_observed"`
	BoundaryLevel                   int                  `json:"boundary_level"`
	BoundaryPassed                  bool                 `json:"boundary_passed"`
}

type claimBenchmarkLevelResult struct {
	Phase             string                           `json:"phase"`
	Concurrency       int                              `json:"concurrency"`
	Invocations       int                              `json:"invocations"`
	WarmupBursts      int                              `json:"warmup_bursts"`
	MeasuredBursts    int                              `json:"measured_bursts"`
	ClaimsAttempted   int                              `json:"claims_attempted"`
	ClaimsSucceeded   int                              `json:"claims_succeeded"`
	ErroredClaims     int                              `json:"errored_claims"`
	QuickChecksPassed int                              `json:"quick_checks_passed"`
	SeededRowsCleaned int                              `json:"seeded_rows_cleaned"`
	Errors            claimBenchmarkErrors             `json:"errors"`
	WarmupErrors      claimBenchmarkErrors             `json:"warmup_errors"`
	ClaimLatencyMS    claimBenchmarkDistribution       `json:"claim_latency_ms"`
	BurstMakespanMS   claimBenchmarkDistribution       `json:"burst_makespan_ms"`
	ClaimsPerSecond   float64                          `json:"claims_per_second"`
	Allocations       claimBenchmarkAllocations        `json:"allocations"`
	Passed            bool                             `json:"passed"`
	ErrorSamples      []string                         `json:"error_samples,omitempty"`
	InvocationResults []claimBenchmarkInvocationResult `json:"invocation_results"`
}

type claimBenchmarkInvocationResult struct {
	Invocation        int                        `json:"invocation"`
	Stacks            int                        `json:"stacks"`
	PinsRequested     int                        `json:"pins_requested"`
	PinsSuccessful    int                        `json:"pins_successful"`
	WarmupBursts      int                        `json:"warmup_bursts"`
	MeasuredBursts    int                        `json:"measured_bursts"`
	ClaimsAttempted   int                        `json:"claims_attempted"`
	ClaimsSucceeded   int                        `json:"claims_succeeded"`
	ErroredClaims     int                        `json:"errored_claims"`
	QuickCheckPassed  bool                       `json:"quick_check_passed"`
	SeededRowsCleaned int                        `json:"seeded_rows_cleaned"`
	Errors            claimBenchmarkErrors       `json:"errors"`
	WarmupErrors      claimBenchmarkErrors       `json:"warmup_errors"`
	ClaimLatencyMS    claimBenchmarkDistribution `json:"claim_latency_ms"`
	BurstMakespanMS   claimBenchmarkDistribution `json:"burst_makespan_ms"`
	ClaimsPerSecond   float64                    `json:"claims_per_second"`
	Allocations       claimBenchmarkAllocations  `json:"allocations"`
	Passed            bool                       `json:"passed"`
	ErrorSamples      []string                   `json:"error_samples,omitempty"`
}

type claimBenchmarkErrors struct {
	Setup            int `json:"setup"`
	Transport        int `json:"transport"`
	Tool             int `json:"tool"`
	Decode           int `json:"decode"`
	EmptyClaims      int `json:"empty_claims"`
	DuplicateClaims  int `json:"duplicate_claims"`
	MissingClaims    int `json:"missing_claims"`
	ForeignClaims    int `json:"foreign_claims"`
	AssignmentOracle int `json:"assignment_oracle"`
	EventOracle      int `json:"event_oracle"`
	StateOracle      int `json:"state_oracle"`
	QuickCheck       int `json:"quick_check"`
}

func (e claimBenchmarkErrors) total() int {
	return e.Setup + e.Transport + e.Tool + e.Decode + e.EmptyClaims + e.DuplicateClaims + e.MissingClaims + e.ForeignClaims + e.AssignmentOracle + e.EventOracle + e.StateOracle + e.QuickCheck
}

func (e claimBenchmarkErrors) claimErrors() int {
	return e.Transport + e.Tool + e.Decode
}

func (e *claimBenchmarkErrors) add(other claimBenchmarkErrors) {
	e.Setup += other.Setup
	e.Transport += other.Transport
	e.Tool += other.Tool
	e.Decode += other.Decode
	e.EmptyClaims += other.EmptyClaims
	e.DuplicateClaims += other.DuplicateClaims
	e.MissingClaims += other.MissingClaims
	e.ForeignClaims += other.ForeignClaims
	e.AssignmentOracle += other.AssignmentOracle
	e.EventOracle += other.EventOracle
	e.StateOracle += other.StateOracle
	e.QuickCheck += other.QuickCheck
}

type claimBenchmarkDistribution struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

type claimBenchmarkAllocations struct {
	TotalBytes     uint64  `json:"total_bytes"`
	TotalAllocs    uint64  `json:"total_allocs"`
	BytesPerClaim  float64 `json:"bytes_per_claim"`
	AllocsPerClaim float64 `json:"allocs_per_claim"`
}

func runClaimBenchmark(ctx context.Context, root string, bundle config.Bundle, cfg claimBenchmarkConfig) (claimBenchmarkResult, error) {
	snapshot := config.BuildSnapshot(bundle)
	environment := collectClaimBenchmarkEnvironment(ctx, root)
	sqliteInfo, err := collectClaimBenchmarkSQLite(ctx, filepath.Join(root, "metadata.db"), bundle)
	if err != nil {
		return claimBenchmarkResult{}, fmt.Errorf("collect SQLite metadata: %w", err)
	}
	result := newClaimBenchmarkResult(cfg, environment, sqliteInfo)
	low, high, failureObserved, err := measureClaimBenchmarkCoarseLevels(ctx, root, snapshot, bundle, cfg, &result)
	if err != nil {
		return claimBenchmarkResult{}, err
	}
	low, high, err = refineClaimBenchmarkLevels(ctx, root, snapshot, bundle, cfg, &result, low, high)
	if err != nil {
		return claimBenchmarkResult{}, err
	}
	return runClaimBenchmarkBoundary(ctx, root, snapshot, bundle, cfg, result, low, high, failureObserved)
}

func newClaimBenchmarkResult(cfg claimBenchmarkConfig, environment claimBenchmarkEnvironment, sqliteInfo claimBenchmarkSQLite) claimBenchmarkResult {
	return claimBenchmarkResult{
		SchemaVersion:       claimBenchmarkSchemaVersion,
		Benchmark:           claimBenchmarkName,
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
		ReproductionCommand: claimBenchmarkReproductionCommand(cfg),
		Environment:         environment,
		SQLite:              sqliteInfo,
		Protocol: claimBenchmarkProtocol{
			CoarseLevels:                []int{},
			RefinedLevels:               []int{},
			ConfiguredMaximum:           cfg.CoarseLevels[len(cfg.CoarseLevels)-1],
			WarmupBurstsPerInvocation:   cfg.WarmupBursts,
			MeasuredBurstsPerInvocation: cfg.MeasuredBursts,
			FreshDatabaseInvocations:    cfg.Invocations,
			BoundaryBurstsPerInvocation: cfg.BoundaryBursts,
			TasksPerBurst:               "one newly seeded task per caller",
			SynchronizedStart:           true,
			IndependentLongLivedStacks:  true,
			FileBackedSharedWAL:         true,
			LatencyIsPassCriterion:      false,
			CorrectnessOracle: []string{
				"zero duplicate, missing, foreign, empty, transport, tool, or decode claim results",
				"one persisted assigned_to value matching the returning caller per seeded task",
				"one task.assigned event with matching assignee per seeded task and no foreign assignment event",
				"unchanged task bucket, plan, wave, and active state plus unchanged plan and wave rows",
				"PRAGMA quick_check returns ok after every fresh-database invocation",
			},
		},
	}
}

func measureClaimBenchmarkCoarseLevels(ctx context.Context, root string, snapshot *config.Snapshot, bundle config.Bundle, cfg claimBenchmarkConfig, result *claimBenchmarkResult) (int, int, bool, error) {
	firstFailure := 0
	previousPass := 0
	for _, level := range cfg.CoarseLevels {
		measured, err := runClaimBenchmarkLevel(ctx, root, snapshot, bundle, cfg, "coarse", level, cfg.MeasuredBursts)
		if err != nil {
			return 0, 0, false, err
		}
		result.Protocol.CoarseLevels = append(result.Protocol.CoarseLevels, level)
		result.Levels = append(result.Levels, measured)
		if firstFailure == 0 {
			if measured.Passed {
				previousPass = level
			} else {
				firstFailure = level
			}
		}
		if firstFailure > 0 && level >= 128 {
			break
		}
	}
	return previousPass, firstFailure, firstFailure > 0, nil
}

func refineClaimBenchmarkLevels(ctx context.Context, root string, snapshot *config.Snapshot, bundle config.Bundle, cfg claimBenchmarkConfig, result *claimBenchmarkResult, low, high int) (int, int, error) {
	for high > 0 && high-low > 1 {
		mid := low + (high-low)/2
		measured, err := runClaimBenchmarkLevel(ctx, root, snapshot, bundle, cfg, "refined", mid, cfg.MeasuredBursts)
		if err != nil {
			return 0, 0, err
		}
		result.Protocol.RefinedLevels = append(result.Protocol.RefinedLevels, mid)
		result.Levels = append(result.Levels, measured)
		if measured.Passed {
			low = mid
		} else {
			high = mid
		}
	}
	return low, high, nil
}

func runClaimBenchmarkBoundary(ctx context.Context, root string, snapshot *config.Snapshot, bundle config.Bundle, cfg claimBenchmarkConfig, result claimBenchmarkResult, low, high int, failureObserved bool) (claimBenchmarkResult, error) {
	for {
		if low == 0 {
			return claimBenchmarkResult{}, fmt.Errorf("no error-free level available for boundary revalidation")
		}
		boundary, err := runClaimBenchmarkLevel(ctx, root, snapshot, bundle, cfg, "boundary", low, cfg.BoundaryBursts)
		if err != nil {
			return claimBenchmarkResult{}, err
		}
		result.Levels = append(result.Levels, boundary)
		if boundary.Passed {
			result.Summary = claimBenchmarkSummary{
				LargestContiguousErrorFreeLevel: low,
				NoFailureObserved:               !failureObserved,
				BoundaryLevel:                   low,
				BoundaryPassed:                  true,
			}
			if !failureObserved {
				result.Summary.Ceiling = fmt.Sprintf(">=%d", low)
				return result, nil
			}
			result.Summary.Ceiling = strconv.Itoa(low)
			result.Summary.FirstFailingLevel = &high
			result.Summary.FirstFailureErrors = claimBenchmarkFailureErrors(result.Levels, high)
			return result, nil
		}

		// A longer boundary sample can expose a low-rate correctness failure
		// that the 30-burst search did not see. Treat that level as the new
		// failing side, fall back to a lower measured pass, and refine again.
		failureObserved = true
		high = low
		low = highestPassingClaimBenchmarkLevelBelow(result.Levels, high)
		low, high, err = refineClaimBenchmarkLevels(ctx, root, snapshot, bundle, cfg, &result, low, high)
		if err != nil {
			return claimBenchmarkResult{}, err
		}
	}
}

func claimBenchmarkFailureErrors(levels []claimBenchmarkLevelResult, concurrency int) claimBenchmarkErrors {
	for i := len(levels) - 1; i >= 0; i-- {
		level := levels[i]
		if level.Concurrency == concurrency && !level.Passed {
			return level.Errors
		}
	}
	return claimBenchmarkErrors{}
}

func highestPassingClaimBenchmarkLevelBelow(levels []claimBenchmarkLevelResult, ceiling int) int {
	highest := 0
	for _, level := range levels {
		if level.Passed && level.Concurrency < ceiling && level.Concurrency > highest {
			highest = level.Concurrency
		}
	}
	return highest
}

func runClaimBenchmarkLevel(ctx context.Context, root string, snapshot *config.Snapshot, bundle config.Bundle, cfg claimBenchmarkConfig, phase string, concurrency, measuredBursts int) (claimBenchmarkLevelResult, error) {
	level := claimBenchmarkLevelResult{Phase: phase, Concurrency: concurrency}
	var latencies, makespans []time.Duration
	for invocation := 1; invocation <= cfg.Invocations; invocation++ {
		path := filepath.Join(root, fmt.Sprintf("claim-%s-%06d-%02d.db", phase, concurrency, invocation))
		run := runClaimBenchmarkInvocation(ctx, path, snapshot, bundle, cfg, invocation, concurrency, measuredBursts)
		level.InvocationResults = append(level.InvocationResults, run.result)
		level.Invocations++
		level.WarmupBursts += run.result.WarmupBursts
		level.MeasuredBursts += run.result.MeasuredBursts
		level.ClaimsAttempted += run.result.ClaimsAttempted
		level.ClaimsSucceeded += run.result.ClaimsSucceeded
		level.ErroredClaims += run.result.ErroredClaims
		level.SeededRowsCleaned += run.result.SeededRowsCleaned
		if run.result.QuickCheckPassed {
			level.QuickChecksPassed++
		}
		level.Errors.add(run.result.Errors)
		level.WarmupErrors.add(run.result.WarmupErrors)
		level.Allocations.TotalBytes += run.result.Allocations.TotalBytes
		level.Allocations.TotalAllocs += run.result.Allocations.TotalAllocs
		level.ErrorSamples = appendLimited(level.ErrorSamples, run.result.ErrorSamples...)
		latencies = append(latencies, run.latencies...)
		makespans = append(makespans, run.makespans...)
	}
	level.ClaimLatencyMS = durationDistribution(latencies)
	level.BurstMakespanMS = durationDistribution(makespans)
	level.ClaimsPerSecond = throughput(level.ClaimsSucceeded, makespans)
	if level.ClaimsAttempted > 0 {
		level.Allocations.BytesPerClaim = float64(level.Allocations.TotalBytes) / float64(level.ClaimsAttempted)
		level.Allocations.AllocsPerClaim = float64(level.Allocations.TotalAllocs) / float64(level.ClaimsAttempted)
	}
	level.Passed = level.Errors.total() == 0 && level.WarmupErrors.total() == 0 &&
		level.Invocations == cfg.Invocations && level.MeasuredBursts == cfg.Invocations*measuredBursts &&
		level.WarmupBursts == cfg.Invocations*cfg.WarmupBursts && level.QuickChecksPassed == cfg.Invocations
	return level, nil
}

type claimInvocationRun struct {
	result    claimBenchmarkInvocationResult
	latencies []time.Duration
	makespans []time.Duration
}

func runClaimBenchmarkInvocation(ctx context.Context, path string, snapshot *config.Snapshot, bundle config.Bundle, cfg claimBenchmarkConfig, invocation, concurrency, measuredBursts int) claimInvocationRun {
	result := claimBenchmarkInvocationResult{Invocation: invocation, Stacks: concurrency, PinsRequested: concurrency}
	projectID, planID, waveID, err := initializeClaimBenchmarkDB(ctx, path, snapshot, bundle)
	if err != nil {
		return claimInvocationSetupError(result, "initialize database", err)
	}
	observer, err := openClaimBenchmarkObserver(path, bundle)
	if err != nil {
		return claimInvocationSetupError(result, "open observer", err)
	}
	defer func() { _ = observer.Close() }()

	stacks, pinCount, err := openClaimBenchmarkStacks(ctx, path, snapshot, bundle, projectID, concurrency)
	result.PinsSuccessful = pinCount
	if err != nil {
		closeClaimBenchmarkStacks(stacks)
		return claimInvocationSetupError(result, "open caller stacks", err)
	}
	defer closeClaimBenchmarkStacks(stacks)

	planBefore, waveBefore, err := readClaimPlanState(ctx, observer, planID, waveID)
	if err != nil {
		return claimInvocationSetupError(result, "read initial plan state", err)
	}

	run := claimInvocationRun{result: result}
	runClaimBenchmarkWarmups(ctx, observer, stacks, projectID, planID, waveID, planBefore, waveBefore, cfg.WarmupBursts, invocation, &run)
	runClaimBenchmarkMeasured(ctx, observer, stacks, projectID, planID, waveID, planBefore, waveBefore, measuredBursts, invocation, concurrency, &run)
	quickOK, err := claimQuickCheck(ctx, observer)
	if err != nil || !quickOK {
		recordClaimBenchmarkQuickCheckFailure(&run, err)
	} else {
		run.result.QuickCheckPassed = true
	}
	finalizeClaimBenchmarkInvocation(&run)
	return run
}

func claimInvocationSetupError(result claimBenchmarkInvocationResult, stage string, err error) claimInvocationRun {
	result.Errors.Setup++
	result.ErrorSamples = appendLimited(result.ErrorSamples, stage+": "+err.Error())
	return claimInvocationRun{result: result}
}

func runClaimBenchmarkWarmups(ctx context.Context, observer *sql.DB, stacks []claimBenchmarkStack, projectID, planID, waveID int64, planBefore claimPlanFingerprint, waveBefore claimWaveFingerprint, claimBenchmarkCount, invocation int, run *claimInvocationRun) {
	for burst := 0; burst < claimBenchmarkCount; burst++ {
		measured := runClaimBenchmarkBurst(ctx, observer, stacks, projectID, planID, waveID, planBefore, waveBefore, false, invocation, burst)
		run.result.WarmupBursts++
		run.result.WarmupErrors.add(measured.errors)
		run.result.SeededRowsCleaned += measured.seededRowsCleaned
		run.result.ErrorSamples = appendLimited(run.result.ErrorSamples, measured.samples...)
	}
}

func runClaimBenchmarkMeasured(ctx context.Context, observer *sql.DB, stacks []claimBenchmarkStack, projectID, planID, waveID int64, planBefore claimPlanFingerprint, waveBefore claimWaveFingerprint, measuredBursts, invocation, concurrency int, run *claimInvocationRun) {
	for burst := 0; burst < measuredBursts; burst++ {
		measured := runClaimBenchmarkBurst(ctx, observer, stacks, projectID, planID, waveID, planBefore, waveBefore, true, invocation, burst)
		run.result.MeasuredBursts++
		run.result.ClaimsAttempted += concurrency
		run.result.ClaimsSucceeded += measured.successes
		run.result.Errors.add(measured.errors)
		run.result.SeededRowsCleaned += measured.seededRowsCleaned
		run.result.Allocations.TotalBytes += measured.allocBytes
		run.result.Allocations.TotalAllocs += measured.allocs
		run.result.ErrorSamples = appendLimited(run.result.ErrorSamples, measured.samples...)
		run.latencies = append(run.latencies, measured.latencies...)
		run.makespans = append(run.makespans, measured.makespan)
	}
}

func recordClaimBenchmarkQuickCheckFailure(run *claimInvocationRun, err error) {
	run.result.Errors.QuickCheck++
	if err != nil {
		run.result.ErrorSamples = appendLimited(run.result.ErrorSamples, "PRAGMA quick_check: "+err.Error())
	}
}

func finalizeClaimBenchmarkInvocation(run *claimInvocationRun) {
	run.result.ClaimLatencyMS = durationDistribution(run.latencies)
	run.result.BurstMakespanMS = durationDistribution(run.makespans)
	run.result.ClaimsPerSecond = throughput(run.result.ClaimsSucceeded, run.makespans)
	if run.result.ClaimsAttempted > 0 {
		run.result.Allocations.BytesPerClaim = float64(run.result.Allocations.TotalBytes) / float64(run.result.ClaimsAttempted)
		run.result.Allocations.AllocsPerClaim = float64(run.result.Allocations.TotalAllocs) / float64(run.result.ClaimsAttempted)
	}
	run.result.ErroredClaims = run.result.Errors.claimErrors()
	run.result.Passed = run.result.Errors.total() == 0 && run.result.WarmupErrors.total() == 0
}

type claimBenchmarkStack struct {
	store   *sqlite.Store
	adapter *Adapter
}

func openClaimBenchmarkStacks(ctx context.Context, path string, snapshot *config.Snapshot, bundle config.Bundle, projectID int64, count int) ([]claimBenchmarkStack, int, error) {
	stacks := make([]claimBenchmarkStack, 0, count)
	pins := 0
	for i := 0; i < count; i++ {
		store, err := sqlite.OpenWithOptions(ctx, path, claimSQLiteOptions(bundle))
		if err != nil {
			return stacks, pins, fmt.Errorf("stack %d store: %w", i, err)
		}
		service := operation.NewService(store, contract.ProjectSelector{ProjectID: projectID})
		service.SetSnapshot(snapshot)
		adapter := NewAdapter(service)
		stacks = append(stacks, claimBenchmarkStack{store: store, adapter: adapter})
		if _, err := store.DataVersion(ctx); err != nil {
			return stacks, pins, fmt.Errorf("stack %d data_version pin: %w", i, err)
		}
		pins++
	}
	return stacks, pins, nil
}

func closeClaimBenchmarkStacks(stacks []claimBenchmarkStack) {
	for i := len(stacks) - 1; i >= 0; i-- {
		_ = stacks[i].store.Close()
	}
}

func claimSQLiteOptions(bundle config.Bundle) sqlite.Options {
	return sqlite.Options{
		BusyTimeoutMs: bundle.Config.SQLite.BusyTimeoutMs,
		CacheSizeKB:   bundle.Config.SQLite.CacheSizeKB,
		MmapSizeBytes: bundle.Config.SQLite.MmapSizeBytes,
	}
}

func initializeClaimBenchmarkDB(ctx context.Context, path string, snapshot *config.Snapshot, bundle config.Bundle) (int64, int64, int64, error) {
	store, err := sqlite.OpenWithOptions(ctx, path, claimSQLiteOptions(bundle))
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = store.Close() }()
	project, err := store.UpsertProject(ctx, "Claim benchmark", "claim-benchmark", filepath.Dir(path))
	if err != nil {
		return 0, 0, 0, err
	}
	plan, err := store.CreatePlan(ctx, project.ID, "claim-ceiling", "Claim ceiling", "")
	if err != nil {
		return 0, 0, 0, err
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "measurement", 0)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(snapshot.Workflow().Buckets) == 0 {
		return 0, 0, 0, fmt.Errorf("benchmark workflow has no buckets")
	}
	return project.ID, plan.ID, wave.ID, nil
}

func openClaimBenchmarkObserver(path string, bundle config.Bundle) (*sql.DB, error) {
	opts := claimSQLiteOptions(bundle)
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=synchronous(1)" +
		fmt.Sprintf("&_pragma=busy_timeout(%d)&_pragma=cache_size(-%d)&_pragma=mmap_size(%d)", opts.BusyTimeoutMs, opts.CacheSizeKB, opts.MmapSizeBytes)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

type claimPlanFingerprint struct {
	ProjectID   int64
	Slug        string
	Name        string
	GoalBody    string
	Status      string
	CreatedAt   string
	UpdatedAt   string
	CompletedAt string
}

type claimWaveFingerprint struct {
	PlanID   int64
	Name     string
	Position int
}

func readClaimPlanState(ctx context.Context, db *sql.DB, planID, waveID int64) (claimPlanFingerprint, claimWaveFingerprint, error) {
	var plan claimPlanFingerprint
	err := db.QueryRowContext(ctx, `
SELECT project_id, slug, name, goal_body, status, created_at, updated_at, COALESCE(completed_at, '')
FROM plans WHERE id = ?`, planID).Scan(&plan.ProjectID, &plan.Slug, &plan.Name, &plan.GoalBody, &plan.Status, &plan.CreatedAt, &plan.UpdatedAt, &plan.CompletedAt)
	if err != nil {
		return claimPlanFingerprint{}, claimWaveFingerprint{}, err
	}
	var wave claimWaveFingerprint
	if err := db.QueryRowContext(ctx, `SELECT plan_id, name, position FROM plan_waves WHERE id = ?`, waveID).Scan(&wave.PlanID, &wave.Name, &wave.Position); err != nil {
		return claimPlanFingerprint{}, claimWaveFingerprint{}, err
	}
	return plan, wave, nil
}

type claimCallResult struct {
	caller   string
	taskID   int64
	claimed  bool
	duration time.Duration
	errType  string
	errText  string
}

type claimSeedState struct {
	taskIDs       []int64
	taskSet       map[int64]struct{}
	bucketID      int64
	planID        int64
	waveID        int64
	eventBaseline int64
}

func runClaimBenchmarkBurst(ctx context.Context, db *sql.DB, stacks []claimBenchmarkStack, projectID, planID, waveID int64, planBefore claimPlanFingerprint, waveBefore claimWaveFingerprint, measured bool, invocation, burst int) claimBurstResult {
	seed, err := seedClaimBenchmarkBurst(ctx, db, projectID, planID, waveID, len(stacks), invocation, burst)
	if err != nil {
		return claimBurstResult{errors: claimBenchmarkErrors{Setup: 1}, samples: []string{"seed burst: " + err.Error()}}
	}

	calls := runClaimBenchmarkCalls(ctx, stacks, planID, invocation, burst, measured)
	burstResult := summarizeClaimBenchmarkCalls(seed, calls)
	oracleErrors, samples := evaluateClaimBenchmarkOracle(ctx, db, seed, burstResult.claimedByTask, projectID, planID, waveID, planBefore, waveBefore)
	burstResult.errors.add(oracleErrors)
	burstResult.samples = appendLimited(burstResult.samples, samples...)
	cleaned, err := cleanupClaimBenchmarkBurst(ctx, db, seed)
	if err != nil {
		burstResult.errors.Setup++
		burstResult.samples = appendLimited(burstResult.samples, "cleanup seeded rows: "+err.Error())
	} else {
		burstResult.seededRowsCleaned = cleaned
	}
	return burstResult
}

type claimBenchmarkCallBatch struct {
	calls      []claimCallResult
	makespan   time.Duration
	allocBytes uint64
	allocs     uint64
}

func runClaimBenchmarkCalls(ctx context.Context, stacks []claimBenchmarkStack, planID int64, invocation, burst int, measured bool) claimBenchmarkCallBatch {
	var before runtime.MemStats
	if measured {
		runtime.ReadMemStats(&before)
	}
	ready := sync.WaitGroup{}
	ready.Add(len(stacks))
	start := make(chan struct{})
	results := make(chan claimCallResult, len(stacks))
	var workers sync.WaitGroup
	for i := range stacks {
		workers.Add(1)
		go func(caller int) {
			defer workers.Done()
			ready.Done()
			<-start
			results <- callClaimBenchmarkTask(ctx, stacks[caller], planID, invocation, burst, caller)
		}(i)
	}
	ready.Wait()
	burstStarted := time.Now()
	close(start)
	workers.Wait()
	makespan := time.Since(burstStarted)
	close(results)
	var after runtime.MemStats
	if measured {
		runtime.ReadMemStats(&after)
	}
	batch := claimBenchmarkCallBatch{makespan: makespan}
	if measured {
		batch.allocBytes = after.TotalAlloc - before.TotalAlloc
		batch.allocs = after.Mallocs - before.Mallocs
	}
	for call := range results {
		batch.calls = append(batch.calls, call)
	}
	return batch
}

func callClaimBenchmarkTask(ctx context.Context, stack claimBenchmarkStack, planID int64, invocation, burst, caller int) claimCallResult {
	model := fmt.Sprintf("claim-bench-%06d", caller)
	started := time.Now()
	result, err := stack.adapter.CallTool(ctx, "plans.claim_next", map[string]any{
		"_agent_model":      model,
		"_agent_session_id": fmt.Sprintf("inv-%02d-burst-%03d", invocation, burst),
		"plan_id":           planID,
	})
	call := claimCallResult{caller: model, duration: time.Since(started)}
	if err != nil {
		call.errType, call.errText = "transport", err.Error()
		return call
	}
	if result.IsError {
		call.errType, call.errText = "tool", snippet(result)
		return call
	}
	var payload struct {
		Claimed bool `json:"claimed"`
		Task    *struct {
			ID int64 `json:"id"`
		} `json:"task"`
	}
	if len(result.Content) != 1 || json.Unmarshal([]byte(result.Content[0].Text), &payload) != nil {
		call.errType, call.errText = "decode", snippet(result)
		return call
	}
	call.claimed = payload.Claimed
	if payload.Task != nil {
		call.taskID = payload.Task.ID
	}
	return call
}

type claimBurstResult struct {
	successes         int
	errors            claimBenchmarkErrors
	latencies         []time.Duration
	makespan          time.Duration
	allocBytes        uint64
	allocs            uint64
	seededRowsCleaned int
	samples           []string
	claimedByTask     map[int64]string
}

func summarizeClaimBenchmarkCalls(seed claimSeedState, batch claimBenchmarkCallBatch) claimBurstResult {
	burstResult := claimBurstResult{
		makespan:      batch.makespan,
		allocBytes:    batch.allocBytes,
		allocs:        batch.allocs,
		claimedByTask: make(map[int64]string, len(batch.calls)),
	}
	returnCounts := make(map[int64]int, len(batch.calls))
	for _, call := range batch.calls {
		burstResult.latencies = append(burstResult.latencies, call.duration)
		switch call.errType {
		case "transport":
			burstResult.errors.Transport++
		case "tool":
			burstResult.errors.Tool++
		case "decode":
			burstResult.errors.Decode++
		}
		if call.errType != "" {
			burstResult.samples = appendLimited(burstResult.samples, fmt.Sprintf("%s: %s", call.errType, call.errText))
			continue
		}
		if !call.claimed || call.taskID == 0 {
			burstResult.errors.EmptyClaims++
			continue
		}
		burstResult.successes++
		returnCounts[call.taskID]++
		if returnCounts[call.taskID] > 1 {
			burstResult.errors.DuplicateClaims++
		}
		if _, ok := seed.taskSet[call.taskID]; !ok {
			burstResult.errors.ForeignClaims++
			continue
		}
		burstResult.claimedByTask[call.taskID] = call.caller
	}
	for _, taskID := range seed.taskIDs {
		if returnCounts[taskID] == 0 {
			burstResult.errors.MissingClaims++
		}
	}
	return burstResult
}

func cleanupClaimBenchmarkBurst(ctx context.Context, db *sql.DB, seed claimSeedState) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	first, last := seed.taskIDs[0], seed.taskIDs[len(seed.taskIDs)-1]
	if _, err := tx.ExecContext(ctx, `
DELETE FROM events
WHERE entity_type = 'task' AND event_type = 'task.assigned' AND entity_id >= ? AND entity_id <= ?`, first, last); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id >= ? AND id <= ?`, first, last)
	if err != nil {
		return 0, err
	}
	cleaned, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if cleaned != int64(len(seed.taskIDs)) {
		return 0, fmt.Errorf("deleted %d tasks, want %d", cleaned, len(seed.taskIDs))
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(cleaned), nil
}

func seedClaimBenchmarkBurst(ctx context.Context, db *sql.DB, projectID, planID, waveID int64, count, invocation, burst int) (claimSeedState, error) {
	var bucketID int64
	if err := db.QueryRowContext(ctx, `SELECT bucket_id FROM tasks WHERE plan_id = ? ORDER BY id LIMIT 1`, planID).Scan(&bucketID); err != nil && err != sql.ErrNoRows {
		return claimSeedState{}, err
	}
	if bucketID == 0 {
		bucketID = 1
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return claimSeedState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	seed := claimSeedState{taskSet: make(map[int64]struct{}, count), bucketID: bucketID, planID: planID, waveID: waveID}
	for caller := 0; caller < count; caller++ {
		result, err := tx.ExecContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state, plan_id, wave_id)
VALUES (?, ?, ?, '', 2, 'active', ?, ?)`, projectID, bucketID, fmt.Sprintf("claim-%02d-%03d-%06d", invocation, burst, caller), planID, waveID)
		if err != nil {
			return claimSeedState{}, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return claimSeedState{}, err
		}
		seed.taskIDs = append(seed.taskIDs, id)
		seed.taskSet[id] = struct{}{}
	}
	if err := tx.Commit(); err != nil {
		return claimSeedState{}, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM events`).Scan(&seed.eventBaseline); err != nil {
		return claimSeedState{}, err
	}
	return seed, nil
}

type claimPersistedTask struct {
	BucketID   int64
	PlanID     int64
	WaveID     int64
	State      string
	AssignedTo string
}

func evaluateClaimBenchmarkOracle(ctx context.Context, db *sql.DB, seed claimSeedState, claimedByTask map[int64]string, projectID, planID, waveID int64, planBefore claimPlanFingerprint, waveBefore claimWaveFingerprint) (claimBenchmarkErrors, []string) {
	oracle, samples, fatal := evaluateClaimBenchmarkTaskOracle(ctx, db, seed, claimedByTask)
	if fatal {
		return oracle, samples
	}
	eventOracle, eventSamples := evaluateClaimBenchmarkEventOracle(ctx, db, seed, claimedByTask, projectID)
	oracle.add(eventOracle)
	samples = appendLimited(samples, eventSamples...)
	stateOracle, stateSamples := evaluateClaimBenchmarkPlanOracle(ctx, db, planID, waveID, planBefore, waveBefore)
	oracle.add(stateOracle)
	samples = appendLimited(samples, stateSamples...)
	if oracle.AssignmentOracle > 0 {
		samples = appendLimited(samples, fmt.Sprintf("assignment oracle failures: %d", oracle.AssignmentOracle))
	}
	if oracle.EventOracle > 0 {
		samples = appendLimited(samples, fmt.Sprintf("event oracle failures: %d", oracle.EventOracle))
	}
	if oracle.StateOracle > 0 {
		samples = appendLimited(samples, fmt.Sprintf("state oracle failures: %d", oracle.StateOracle))
	}
	return oracle, samples
}

func evaluateClaimBenchmarkTaskOracle(ctx context.Context, db *sql.DB, seed claimSeedState, claimedByTask map[int64]string) (claimBenchmarkErrors, []string, bool) {
	var oracle claimBenchmarkErrors
	var samples []string
	rows, err := db.QueryContext(ctx, `
SELECT id, COALESCE(bucket_id, 0), COALESCE(plan_id, 0), COALESCE(wave_id, 0), state, COALESCE(assigned_to, '')
FROM tasks WHERE id >= ? AND id <= ? ORDER BY id`, seed.taskIDs[0], seed.taskIDs[len(seed.taskIDs)-1])
	if err != nil {
		oracle.StateOracle++
		return oracle, appendLimited(samples, "read persisted tasks: "+err.Error()), true
	}
	persisted := make(map[int64]claimPersistedTask, len(seed.taskIDs))
	for rows.Next() {
		var id int64
		var task claimPersistedTask
		if err := rows.Scan(&id, &task.BucketID, &task.PlanID, &task.WaveID, &task.State, &task.AssignedTo); err != nil {
			_ = rows.Close()
			oracle.StateOracle++
			return oracle, appendLimited(samples, "scan persisted task: "+err.Error()), true
		}
		persisted[id] = task
	}
	if err := rows.Close(); err != nil {
		oracle.StateOracle++
		samples = appendLimited(samples, "close persisted tasks: "+err.Error())
	}
	for _, id := range seed.taskIDs {
		task, ok := persisted[id]
		if !ok {
			oracle.StateOracle++
			oracle.AssignmentOracle++
			continue
		}
		if task.BucketID != seed.bucketID || task.PlanID != seed.planID || task.WaveID != seed.waveID || task.State != "active" {
			oracle.StateOracle++
		}
		wantAssignee := claimedByTask[id]
		if wantAssignee == "" || task.AssignedTo != wantAssignee {
			oracle.AssignmentOracle++
		}
	}
	return oracle, samples, false
}

func evaluateClaimBenchmarkEventOracle(ctx context.Context, db *sql.DB, seed claimSeedState, claimedByTask map[int64]string, projectID int64) (claimBenchmarkErrors, []string) {
	var oracle claimBenchmarkErrors
	var samples []string
	eventRows, err := db.QueryContext(ctx, `
SELECT COALESCE(entity_id, 0), payload FROM events
WHERE id > ? AND project_id = ? AND event_type = 'task.assigned'
ORDER BY id`, seed.eventBaseline, projectID)
	if err != nil {
		oracle.EventOracle++
		return oracle, appendLimited(samples, "read assignment events: "+err.Error())
	}
	eventCount := make(map[int64]int, len(seed.taskIDs))
	for eventRows.Next() {
		var taskID int64
		var payloadText string
		if err := eventRows.Scan(&taskID, &payloadText); err != nil {
			oracle.EventOracle++
			continue
		}
		eventCount[taskID]++
		if _, ok := seed.taskSet[taskID]; !ok {
			oracle.EventOracle++
			oracle.ForeignClaims++
			continue
		}
		var payload struct {
			Assignee string `json:"assignee"`
		}
		if json.Unmarshal([]byte(payloadText), &payload) != nil || payload.Assignee != claimedByTask[taskID] {
			oracle.EventOracle++
		}
	}
	if err := eventRows.Close(); err != nil {
		oracle.EventOracle++
	}
	for _, id := range seed.taskIDs {
		if eventCount[id] != 1 {
			oracle.EventOracle++
		}
	}
	return oracle, samples
}

func evaluateClaimBenchmarkPlanOracle(ctx context.Context, db *sql.DB, planID, waveID int64, planBefore claimPlanFingerprint, waveBefore claimWaveFingerprint) (claimBenchmarkErrors, []string) {
	var oracle claimBenchmarkErrors
	var samples []string
	planAfter, waveAfter, err := readClaimPlanState(ctx, db, planID, waveID)
	if err != nil || planAfter != planBefore || waveAfter != waveBefore {
		oracle.StateOracle++
		if err != nil {
			samples = appendLimited(samples, "read final plan state: "+err.Error())
		}
	}
	return oracle, samples
}

func claimQuickCheck(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA quick_check`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	seen := false
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return false, err
		}
		seen = true
		if value != "ok" {
			return false, nil
		}
	}
	return seen, rows.Err()
}

func durationDistribution(values []time.Duration) claimBenchmarkDistribution {
	if len(values) == 0 {
		return claimBenchmarkDistribution{}
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	toMS := func(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }
	percentile := func(p float64) float64 {
		index := int(math.Ceil(p*float64(len(sorted)))) - 1
		if index < 0 {
			index = 0
		}
		return toMS(sorted[index])
	}
	return claimBenchmarkDistribution{P50: percentile(0.50), P95: percentile(0.95), P99: percentile(0.99), Max: toMS(sorted[len(sorted)-1])}
}

func throughput(successes int, makespans []time.Duration) float64 {
	var total time.Duration
	for _, duration := range makespans {
		total += duration
	}
	if total <= 0 {
		return 0
	}
	return float64(successes) / total.Seconds()
}

func appendLimited(existing []string, values ...string) []string {
	const limit = 10
	for _, value := range values {
		if value == "" || len(existing) >= limit {
			continue
		}
		existing = append(existing, value)
	}
	return existing
}

func collectClaimBenchmarkEnvironment(ctx context.Context, root string) claimBenchmarkEnvironment {
	environment := claimBenchmarkEnvironment{
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		GoVersion:       runtime.Version(),
		LogicalCPUs:     runtime.NumCPU(),
		GOMAXPROCS:      runtime.GOMAXPROCS(0),
		CPUModel:        procValue("/proc/cpuinfo", "model name"),
		KernelRelease:   strings.TrimSpace(readOptionalFile("/proc/sys/kernel/osrelease")),
		MemoryTotalKiB:  procInt64("/proc/meminfo", "MemTotal"),
		DatabaseStorage: "file-backed WAL databases under " + filepath.Clean(root),
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				environment.SourceRevision = setting.Value
			case "vcs.modified":
				environment.SourceModified = setting.Value == "true"
			}
		}
	}
	if repoRoot, err := claimBenchmarkRepoRoot(); err == nil {
		revision := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
		revision.Dir = repoRoot
		if output, err := revision.Output(); err == nil {
			environment.SourceRevision = strings.TrimSpace(string(output))
		}
		status := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=normal")
		status.Dir = repoRoot
		if output, err := status.Output(); err == nil {
			environment.SourceModified = strings.TrimSpace(string(output)) != ""
		}
	}
	return environment
}

func collectClaimBenchmarkSQLite(ctx context.Context, path string, bundle config.Bundle) (claimBenchmarkSQLite, error) {
	store, err := sqlite.OpenWithOptions(ctx, path, claimSQLiteOptions(bundle))
	if err != nil {
		return claimBenchmarkSQLite{}, err
	}
	if _, err := store.DataVersion(ctx); err != nil {
		_ = store.Close()
		return claimBenchmarkSQLite{}, err
	}
	if err := store.Close(); err != nil {
		return claimBenchmarkSQLite{}, err
	}
	db, err := openClaimBenchmarkObserver(path, bundle)
	if err != nil {
		return claimBenchmarkSQLite{}, err
	}
	defer func() { _ = db.Close() }()
	var version, journalMode string
	var foreignKeys, synchronous, busyTimeout, cacheSize, tempStore int
	var mmapSize int64
	queries := []struct {
		query string
		dest  any
	}{
		{"SELECT sqlite_version()", &version},
		{"PRAGMA journal_mode", &journalMode},
		{"PRAGMA foreign_keys", &foreignKeys},
		{"PRAGMA synchronous", &synchronous},
		{"PRAGMA busy_timeout", &busyTimeout},
		{"PRAGMA cache_size", &cacheSize},
		{"PRAGMA mmap_size", &mmapSize},
		{"PRAGMA temp_store", &tempStore},
	}
	for _, query := range queries {
		if err := db.QueryRowContext(ctx, query.query).Scan(query.dest); err != nil {
			return claimBenchmarkSQLite{}, fmt.Errorf("%s: %w", query.query, err)
		}
	}
	return claimBenchmarkSQLite{
		Version:       version,
		Driver:        "modernc.org/sqlite",
		DriverVersion: claimSQLiteDriverVersion(),
		Pragmas: map[string]any{
			"journal_mode": journalMode,
			"foreign_keys": foreignKeys,
			"synchronous":  synchronous,
			"busy_timeout": busyTimeout,
			"cache_size":   cacheSize,
			"mmap_size":    mmapSize,
			"temp_store":   tempStore,
		},
		Pool: claimBenchmarkPoolInfo{
			StackShape:                 "one sqlite.Store, operation.Service, and mcp.Adapter per caller",
			SharedStore:                false,
			MaxOpenConnectionsPerStore: 3,
			MaxIdleConnectionsPerStore: 2,
			DataVersionPinRequested:    true,
			PinnedConnectionsPerStore:  1,
			OrdinarySlotsAfterPin:      2,
		},
	}, nil
}

func claimSQLiteDriverVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range info.Deps {
			if dependency.Path == "modernc.org/sqlite" {
				return dependency.Version
			}
		}
	}
	root, err := claimBenchmarkRepoRoot()
	if err == nil {
		for _, line := range strings.Split(readOptionalFile(filepath.Join(root, "go.mod")), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "modernc.org/sqlite" {
				return fields[1]
			}
		}
	}
	return "unknown"
}

func procValue(path, key string) string {
	for _, line := range strings.Split(readOptionalFile(path), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(name) == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func procInt64(path, key string) int64 {
	value := procValue(path, key)
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	parsed, _ := strconv.ParseInt(fields[0], 10, 64)
	return parsed
}

func readOptionalFile(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func claimBenchmarkOutputPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	root, err := claimBenchmarkRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, path), nil
}

func claimBenchmarkRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found from %s", dir)
		}
		dir = parent
	}
}

func validateClaimBenchmarkResult(result claimBenchmarkResult) error {
	if result.SchemaVersion != claimBenchmarkSchemaVersion || result.Benchmark != claimBenchmarkName {
		return fmt.Errorf("unexpected schema/name: %d %q", result.SchemaVersion, result.Benchmark)
	}
	if _, err := time.Parse(time.RFC3339, result.GeneratedAt); err != nil {
		return fmt.Errorf("invalid generated_at %q: %w", result.GeneratedAt, err)
	}
	if strings.TrimSpace(result.ReproductionCommand) == "" {
		return fmt.Errorf("reproduction command is required")
	}
	if err := validateClaimBenchmarkProtocol(result); err != nil {
		return err
	}
	if err := validateClaimBenchmarkEnvironment(result); err != nil {
		return err
	}
	return validateClaimBenchmarkLevels(result)
}

func validateClaimBenchmarkProtocol(result claimBenchmarkResult) error {
	protocol := result.Protocol
	if len(protocol.CoarseLevels) == 0 || protocol.WarmupBurstsPerInvocation <= 0 || protocol.MeasuredBurstsPerInvocation <= 0 || protocol.FreshDatabaseInvocations <= 0 || protocol.BoundaryBurstsPerInvocation <= 0 {
		return fmt.Errorf("incomplete protocol: %+v", protocol)
	}
	if protocol.ConfiguredMaximum < protocol.CoarseLevels[len(protocol.CoarseLevels)-1] ||
		protocol.TasksPerBurst == "" || !protocol.SynchronizedStart || !protocol.IndependentLongLivedStacks ||
		!protocol.FileBackedSharedWAL || protocol.LatencyIsPassCriterion || len(protocol.CorrectnessOracle) != 5 {
		return fmt.Errorf("invalid protocol metadata: %+v", protocol)
	}
	return nil
}

func validateClaimBenchmarkEnvironment(result claimBenchmarkResult) error {
	environment := result.Environment
	if environment.GOOS == "" || environment.GOARCH == "" || environment.GoVersion == "" ||
		environment.LogicalCPUs <= 0 || environment.GOMAXPROCS <= 0 || environment.SourceRevision == "" ||
		environment.DatabaseStorage == "" {
		return fmt.Errorf("incomplete environment metadata: %+v", environment)
	}
	pool := result.SQLite.Pool
	if result.SQLite.Version == "" || result.SQLite.Driver == "" || result.SQLite.DriverVersion == "" ||
		pool.StackShape == "" || pool.SharedStore || pool.MaxOpenConnectionsPerStore <= 0 ||
		pool.MaxIdleConnectionsPerStore <= 0 || !pool.DataVersionPinRequested ||
		pool.PinnedConnectionsPerStore <= 0 || pool.OrdinarySlotsAfterPin <= 0 {
		return fmt.Errorf("incomplete SQLite/pool metadata: %+v", result.SQLite)
	}
	for _, pragma := range []string{"journal_mode", "foreign_keys", "synchronous", "busy_timeout", "cache_size", "mmap_size", "temp_store"} {
		if _, ok := result.SQLite.Pragmas[pragma]; !ok {
			return fmt.Errorf("missing SQLite PRAGMA metadata %q", pragma)
		}
	}
	return nil
}

func validateClaimBenchmarkLevels(result claimBenchmarkResult) error {
	protocol := result.Protocol
	coarseSeen := make(map[int]bool, len(protocol.CoarseLevels))
	boundarySeen := false
	for _, level := range result.Levels {
		if err := validateClaimBenchmarkLevel(level, protocol); err != nil {
			return err
		}
		if level.Phase == "boundary" {
			if level.Concurrency == result.Summary.BoundaryLevel {
				boundarySeen = true
			}
		}
		if level.Phase == "coarse" {
			coarseSeen[level.Concurrency] = true
		}
	}
	for _, level := range protocol.CoarseLevels {
		if !coarseSeen[level] {
			return fmt.Errorf("missing coarse level %d", level)
		}
	}
	return validateClaimBenchmarkSummary(result.Summary, boundarySeen)
}

func validateClaimBenchmarkLevel(level claimBenchmarkLevelResult, protocol claimBenchmarkProtocol) error {
	expectedMeasured := protocol.MeasuredBurstsPerInvocation * protocol.FreshDatabaseInvocations
	if level.Phase == "boundary" {
		expectedMeasured = protocol.BoundaryBurstsPerInvocation * protocol.FreshDatabaseInvocations
	}
	if level.Invocations != protocol.FreshDatabaseInvocations || level.WarmupBursts != protocol.WarmupBurstsPerInvocation*protocol.FreshDatabaseInvocations || level.MeasuredBursts != expectedMeasured {
		return fmt.Errorf("level %s/%d sample counts = invocations:%d warmups:%d measured:%d, want %d/%d/%d", level.Phase, level.Concurrency, level.Invocations, level.WarmupBursts, level.MeasuredBursts, protocol.FreshDatabaseInvocations, protocol.WarmupBurstsPerInvocation*protocol.FreshDatabaseInvocations, expectedMeasured)
	}
	if level.Passed && (level.Errors.total() != 0 || level.WarmupErrors.total() != 0) {
		return fmt.Errorf("level %s/%d marked passed with errors", level.Phase, level.Concurrency)
	}
	if level.Passed && level.QuickChecksPassed != protocol.FreshDatabaseInvocations {
		return fmt.Errorf("level %s/%d passed %d quick checks, want %d", level.Phase, level.Concurrency, level.QuickChecksPassed, protocol.FreshDatabaseInvocations)
	}
	return nil
}

func validateClaimBenchmarkSummary(summary claimBenchmarkSummary, boundarySeen bool) error {
	if !boundarySeen || !summary.BoundaryPassed {
		return fmt.Errorf("final boundary %d was not successfully revalidated", summary.BoundaryLevel)
	}
	if summary.NoFailureObserved {
		want := fmt.Sprintf(">=%d", summary.LargestContiguousErrorFreeLevel)
		if summary.Ceiling != want || summary.FirstFailingLevel != nil {
			return fmt.Errorf("no-failure summary = %+v, want ceiling %s and nil first failure", summary, want)
		}
	}
	return nil
}
