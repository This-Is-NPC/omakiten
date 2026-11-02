package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"

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
func TickHLC(last HLC, wallMS int64, device UID) HLC {
	if wallMS > last.Wall {
		return HLC{Wall: wallMS, Device: device}
	}
	return bumpHLC(last.Wall, last.Counter, device)
}

// ObserveHLC stamps the receipt of a remote stamp; the result is strictly
// greater than both last and remote.
func ObserveHLC(last, remote HLC, wallMS int64, device UID) HLC {
	wall := max(last.Wall, remote.Wall, wallMS)
	switch {
	case wall == last.Wall && wall == remote.Wall:
		return bumpHLC(wall, max(last.Counter, remote.Counter), device)
	case wall == last.Wall:
		return bumpHLC(wall, last.Counter, device)
	case wall == remote.Wall:
		return bumpHLC(wall, remote.Counter, device)
	default:
		return HLC{Wall: wall, Device: device}
	}
}

// bumpHLC returns the stamp after (wall, counter). A counter at MaxUint32
// carries into Wall with Counter reset, so the clock never refuses and stays
// strictly increasing.
func bumpHLC(wall int64, counter uint32, device UID) HLC {
	if counter == math.MaxUint32 {
		return HLC{Wall: wall + 1, Device: device}
	}
	return HLC{Wall: wall, Counter: counter + 1, Device: device}
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
