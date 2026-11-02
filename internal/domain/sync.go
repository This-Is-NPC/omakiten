package domain

import (
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// UID is a global record identity: a UUIDv7 in canonical lowercase text.
type UID string

// NewUID returns a fresh UUIDv7 identity.
func NewUID() UID {
	return UID(uuid.Must(uuid.NewV7()).String())
}

// ParseUID accepts only the canonical lowercase text of a UUID version 7.
func ParseUID(s string) (UID, error) {
	u, err := uuid.Parse(s)
	if err != nil || u.String() != s || u.Version() != 7 || u.Variant() != uuid.RFC4122 {
		return "", syncInvalid("uid", s, "must be a canonical lowercase UUIDv7")
	}
	return UID(s), nil
}

const (
	hlcWallDigits    = 15
	hlcCounterDigits = 10

	// MaxHLCWall is the largest wall time (unix ms) an HLC string can carry.
	MaxHLCWall int64 = 1e15 - 1

	// MaxHLCDrift is how far a remote stamp may run ahead of the local wall clock.
	MaxHLCDrift = 24 * time.Hour
)

// HLC is a hybrid logical clock stamp. The zero value means "no stamp yet".
type HLC struct {
	Wall    int64  `json:"wall"`
	Counter uint32 `json:"counter"`
	Device  UID    `json:"device,omitempty"`
}

// String renders the stamp fixed-width, so byte order equals Compare order.
func (h HLC) String() string {
	return fmt.Sprintf("%0*d-%0*d-%s", hlcWallDigits, h.Wall, hlcCounterDigits, h.Counter, h.Device)
}

// ParseHLC parses the output of HLC.String.
func ParseHLC(s string) (HLC, error) {
	const head = hlcWallDigits + 1 + hlcCounterDigits + 1
	if len(s) < head || s[hlcWallDigits] != '-' || s[head-1] != '-' {
		return HLC{}, syncInvalid("hlc", s, "must be <wall>-<counter>-<device>")
	}
	wall, err := parseDigits(s[:hlcWallDigits])
	if err != nil {
		return HLC{}, syncInvalid("hlc", s, "wall must be fixed-width decimal")
	}
	counter, err := parseDigits(s[hlcWallDigits+1 : head-1])
	if err != nil || counter > math.MaxUint32 {
		return HLC{}, syncInvalid("hlc", s, "counter must be fixed-width decimal within uint32")
	}
	h := HLC{Wall: wall, Counter: uint32(counter)}
	if device := s[head:]; device != "" {
		if h.Device, err = ParseUID(device); err != nil {
			return HLC{}, syncInvalid("hlc", s, "device must be a UUIDv7")
		}
	}
	return h, nil
}

func parseDigits(s string) (int64, error) {
	if strings.Trim(s, "0123456789") != "" {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseInt(s, 10, 64)
}

// Compare orders by Wall, then Counter, then Device and returns -1, 0 or 1.
func (h HLC) Compare(other HLC) int {
	switch {
	case h.Wall != other.Wall:
		return cmpInt(h.Wall, other.Wall)
	case h.Counter != other.Counter:
		return cmpInt(int64(h.Counter), int64(other.Counter))
	default:
		return strings.Compare(string(h.Device), string(other.Device))
	}
}

func cmpInt(a, b int64) int {
	if a < b {
		return -1
	}
	return 1
}

// TickHLC stamps a local event. The caller persists the result as the next last.
// It fails when the clock cannot advance within MaxHLCWall.
func TickHLC(last HLC, wallMS int64, device UID) (HLC, error) {
	if wallMS > last.Wall {
		return boundHLC(HLC{Wall: wallMS, Device: device})
	}
	return bumpHLC(last.Wall, last.Counter, device)
}

// ObserveHLC stamps the receipt of a remote stamp; the result is strictly
// greater than both last and remote. It fails when the clock cannot advance
// within MaxHLCWall. Remote drift is bounded by syncproto.ValidateChange.
func ObserveHLC(last, remote HLC, wallMS int64, device UID) (HLC, error) {
	wall := max(last.Wall, remote.Wall, wallMS)
	switch {
	case wall == last.Wall && wall == remote.Wall:
		return bumpHLC(wall, max(last.Counter, remote.Counter), device)
	case wall == last.Wall:
		return bumpHLC(wall, last.Counter, device)
	case wall == remote.Wall:
		return bumpHLC(wall, remote.Counter, device)
	default:
		return boundHLC(HLC{Wall: wall, Device: device})
	}
}

// bumpHLC returns the stamp after (wall, counter). A counter at MaxUint32
// carries into Wall with Counter reset.
func bumpHLC(wall int64, counter uint32, device UID) (HLC, error) {
	if counter == math.MaxUint32 {
		return boundHLC(HLC{Wall: wall + 1, Device: device})
	}
	return boundHLC(HLC{Wall: wall, Counter: counter + 1, Device: device})
}

// boundHLC returns h, or an error when its wall exceeds MaxHLCWall.
func boundHLC(h HLC) (HLC, error) {
	if h.Wall > MaxHLCWall {
		return HLC{}, NewError(ErrValidation,
			"hlc clock cannot advance: wall time is at its maximum",
			map[string]any{"field": "hlc", "wall": h.Wall})
	}
	return h, nil
}

// SyncScope says whether a record leaves the device.
type SyncScope string

const (
	SyncScopeLocal  SyncScope = "local"
	SyncScopeSynced SyncScope = "synced"
)

// SyncScopes lists every scope; the first is the default.
var SyncScopes = []SyncScope{SyncScopeLocal, SyncScopeSynced}

// ParseSyncScope parses a scope name.
func ParseSyncScope(s string) (SyncScope, error) {
	for _, scope := range SyncScopes {
		if string(scope) == s {
			return scope, nil
		}
	}
	return "", syncInvalid("sync_scope", s, "must be local or synced")
}

func syncInvalid(field, value, reason string) error {
	return NewError(ErrValidation,
		fmt.Sprintf("invalid %s %q: %s", field, value, reason),
		map[string]any{"field": field, "value": value})
}

// SyncOp is the kind of row change a SyncChange carries.
type SyncOp string

const (
	SyncOpUpsert SyncOp = "upsert"
	SyncOpDelete SyncOp = "delete"
)

// SyncTables lists the synced tables in apply order: parents before children.
var SyncTables = []string{
	"projects", "plans", "plan_waves", "tasks", "errors", "solutions", "events",
	"task_dependencies", "task_tags", "error_tags", "event_tags",
}

// SyncChange is one row version on the wire. Data holds column values with
// every foreign key x_id sent as x_uid (tags as tag_name).
type SyncChange struct {
	Table  string         `json:"table"`
	RowUID string         `json:"row_uid"`
	Op     SyncOp         `json:"op"`
	HLC    string         `json:"hlc"`
	Device UID            `json:"device"`
	Data   map[string]any `json:"data,omitempty"`
}

// SyncProjectInfo is the identity and shared fields of a synced project.
type SyncProjectInfo struct {
	UID         UID    `json:"uid"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	KitKey      string `json:"kit_key"`
}

// SyncProject is a local synced project with its replication cursors.
type SyncProject struct {
	ID         int64           `json:"id"`
	Info       SyncProjectInfo `json:"info"`
	PushedHLC  string          `json:"pushed_hlc"`
	PullCursor int64           `json:"pull_cursor"`
}

// SyncProjectReport counts the changes one sync moved for a project.
type SyncProjectReport struct {
	Slug     string `json:"slug"`
	Pushed   int    `json:"pushed"`
	Pulled   int    `json:"pulled"`
	Rejected int    `json:"rejected"`
	Error    string `json:"error,omitempty"`
}

// SyncReport is the outcome of one sync run.
type SyncReport struct {
	Projects []SyncProjectReport `json:"projects"`
}

// Failure returns a sync_failed error carrying the report when any project
// recorded an error, and nil when every project synced.
func (r SyncReport) Failure() error {
	failed := 0
	for _, p := range r.Projects {
		if p.Error != "" {
			failed++
		}
	}
	if failed == 0 {
		return nil
	}
	return NewError(ErrSyncFailed,
		fmt.Sprintf("%d of %d projects failed to sync; details.projects holds each project's error", failed, len(r.Projects)),
		map[string]any{"projects": r.Projects})
}

// ValidateSyncRemoteURL accepts an absolute https URL with a host, or an http
// URL whose host is loopback unless allowInsecure is set.
func ValidateSyncRemoteURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return syncInvalid("url", raw, "want https://host[:port]")
	}
	if u.Scheme == "http" && !allowInsecure && !isLoopbackHost(u.Hostname()) {
		return syncInvalid("url", raw, "http is only allowed for loopback hosts; use https, or set OMAKITEN_SYNC_INSECURE=1 to allow plain http")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// ValidateSyncProjectInfo refuses project info a peer must not be trusted with:
// a malformed UID or slug, an empty name, or control characters in the text.
// Only the description may hold newlines and tabs.
func ValidateSyncProjectInfo(info SyncProjectInfo) error {
	if _, err := ParseUID(string(info.UID)); err != nil {
		return err
	}
	if info.Slug == "" || Slugify(info.Slug) != info.Slug {
		return syncInvalid("slug", info.Slug, "must be a lowercase slug")
	}
	if info.Name == "" {
		return syncInvalid("name", "", "must not be empty")
	}
	for field, value := range map[string]string{"name": info.Name, "kit_key": info.KitKey} {
		if strings.ContainsFunc(value, unicode.IsControl) {
			return syncInvalid(field, value, "must not contain control characters")
		}
	}
	if strings.ContainsFunc(info.Description, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t'
	}) {
		return syncInvalid("description", info.Description, "must not contain control characters")
	}
	return nil
}
