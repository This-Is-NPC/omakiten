package domain

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	devA UID = "01900000-0000-7000-8000-00000000000a"
	devB UID = "01900000-0000-7000-8000-00000000000b"
)

func TestNewUIDIsParseable(t *testing.T) {
	a, b := NewUID(), NewUID()
	if a == b {
		t.Fatalf("NewUID repeated %q", a)
	}
	if got, err := ParseUID(string(a)); err != nil || got != a {
		t.Fatalf("ParseUID(%q) = %q, %v", a, got, err)
	}
}

func TestParseUIDRefuses(t *testing.T) {
	v4 := uuid.New().String()
	for name, in := range map[string]string{
		"empty":     "",
		"garbage":   "not-a-uid",
		"v4":        v4,
		"uppercase": "01900000-0000-7000-8000-00000000000A",
		"no hyphen": "0190000000007000800000000000000a",
		"urn":       "urn:uuid:" + string(devA),
		"braces":    "{" + string(devA) + "}",
		"bad var":   "01900000-0000-7000-c000-00000000000a",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseUID(in); err == nil {
				t.Fatalf("ParseUID(%q) accepted", in)
			}
		})
	}
}

func requireAfter(t *testing.T, got, prior HLC) {
	t.Helper()
	if got.Wall < prior.Wall || (got.Wall == prior.Wall && got.Counter <= prior.Counter) {
		t.Fatalf("%+v not strictly after %+v", got, prior)
	}
}

func TestTickHLC(t *testing.T) {
	tests := []struct {
		name string
		last HLC
		wall int64
		want HLC
	}{
		{"zero", HLC{}, 100, HLC{100, 0, devA}},
		{"advancing", HLC{100, 5, devB}, 200, HLC{200, 0, devA}},
		{"stalled", HLC{100, 5, devB}, 100, HLC{100, 6, devA}},
		{"backwards", HLC{100, 5, devB}, 50, HLC{100, 6, devA}},
		{"counter overflow carries", HLC{100, math.MaxUint32, devA}, 100, HLC{101, 0, devA}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TickHLC(tc.last, tc.wall, devA)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			requireAfter(t, got, tc.last)
		})
	}
}

func TestObserveHLC(t *testing.T) {
	tests := []struct {
		name         string
		last, remote HLC
		wall         int64
		want         HLC
	}{
		{"wall clock ahead of both", HLC{100, 3, devA}, HLC{110, 4, devB}, 200, HLC{200, 0, devA}},
		{"remote ahead of local", HLC{100, 3, devA}, HLC{150, 4, devB}, 120, HLC{150, 5, devA}},
		{"remote behind local", HLC{150, 3, devA}, HLC{100, 9, devB}, 120, HLC{150, 4, devA}},
		{"equal walls take larger counter", HLC{150, 3, devA}, HLC{150, 9, devB}, 100, HLC{150, 10, devA}},
		{"equal walls local counter larger", HLC{150, 9, devA}, HLC{150, 3, devB}, 150, HLC{150, 10, devA}},
		{"remote overflow carries", HLC{10, 0, devA}, HLC{150, math.MaxUint32, devB}, 100, HLC{151, 0, devA}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ObserveHLC(tc.last, tc.remote, tc.wall, devA)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for _, prior := range []HLC{tc.last, tc.remote} {
				requireAfter(t, got, prior)
			}
		})
	}
}

func TestHLCStringOrderMatchesCompare(t *testing.T) {
	stamps := []HLC{
		{},
		{0, 1, ""},
		{0, 1, devA},
		{9, 0, devA},
		{10, 0, devA},
		{10, 2, devA},
		{10, 10, devA},
		{10, 10, devB},
		{1762000000000, 0, devA},
		{1762000000000, math.MaxUint32, devB},
		{MaxHLCWall, math.MaxUint32, devB},
	}
	for _, a := range stamps {
		for _, b := range stamps {
			want := a.Compare(b)
			got := 0
			switch {
			case a.String() < b.String():
				got = -1
			case a.String() > b.String():
				got = 1
			}
			if got != want {
				t.Errorf("%q vs %q: lexical %d, Compare %d", a, b, got, want)
			}
		}
	}
	shuffled := []HLC{stamps[5], stamps[2], stamps[9], stamps[0], stamps[7]}
	sort.Slice(shuffled, func(i, j int) bool { return shuffled[i].String() < shuffled[j].String() })
	if !sort.SliceIsSorted(shuffled, func(i, j int) bool { return shuffled[i].Compare(shuffled[j]) < 0 }) {
		t.Fatalf("lexical sort is not Compare sort: %v", shuffled)
	}
}

func TestHLCParseRoundTrip(t *testing.T) {
	for _, h := range []HLC{{}, {1, 2, devA}, {1762000000000, math.MaxUint32, devB}, {MaxHLCWall, 0, devA}} {
		got, err := ParseHLC(h.String())
		if err != nil || got != h {
			t.Errorf("round trip %+v: got %+v, %v", h, got, err)
		}
	}
}

func TestParseHLCRefuses(t *testing.T) {
	for name, in := range map[string]string{
		"empty":         "",
		"short":         "1-2-3",
		"unpadded":      "1-2-" + string(devA),
		"bad separator": "000000000000001_0000000002-" + string(devA),
		"signed wall":   "-00000000000001-0000000002-" + string(devA),
		"letters":       "00000000000000a-0000000002-" + string(devA),
		"counter big":   "000000000000001-4294967296-" + string(devA),
		"bad device":    "000000000000001-0000000002-nope",
		"v4 device":     "000000000000001-0000000002-" + uuid.New().String(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseHLC(in); err == nil {
				t.Fatalf("ParseHLC(%q) accepted", in)
			}
		})
	}
}

func TestParseSyncScope(t *testing.T) {
	for _, s := range SyncScopes {
		if got, err := ParseSyncScope(string(s)); err != nil || got != s {
			t.Errorf("ParseSyncScope(%q) = %q, %v", s, got, err)
		}
	}
	if SyncScopes[0] != SyncScopeLocal {
		t.Errorf("default scope = %q, want local", SyncScopes[0])
	}
	for _, in := range []string{"", "Local", "remote", " synced"} {
		if _, err := ParseSyncScope(in); err == nil {
			t.Errorf("ParseSyncScope(%q) accepted", in)
		}
	}
}

func TestSyncTypesJSONRoundTrip(t *testing.T) {
	dev := NewUID()
	in := SyncProject{
		ID:         7,
		Info:       SyncProjectInfo{UID: NewUID(), Name: "N", Slug: "n", Description: "d", KitKey: "k"},
		PushedHLC:  "h",
		PullCursor: 9,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"pushed_hlc"`, `"pull_cursor"`, `"kit_key"`, `"uid"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("missing %s in %s", key, raw)
		}
	}
	var out SyncProject
	if err := json.Unmarshal(raw, &out); err != nil || out != in {
		t.Fatalf("round trip = %+v, %v", out, err)
	}

	change := SyncChange{Table: "tasks", RowUID: "r", Op: SyncOpUpsert, HLC: "h", Device: dev, Data: map[string]any{"title": "x"}}
	raw, err = json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"row_uid"`, `"op":"upsert"`, `"device"`, `"data"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("missing %s in %s", key, raw)
		}
	}
	var back SyncChange
	if err := json.Unmarshal(raw, &back); err != nil || !reflect.DeepEqual(back, change) {
		t.Fatalf("round trip = %+v, %v", back, err)
	}

	report := SyncReport{Projects: []SyncProjectReport{{Slug: "s", Pushed: 1, Pulled: 2, Rejected: 3, Error: "e"}}}
	raw, _ = json.Marshal(report)
	if string(raw) != `{"projects":[{"slug":"s","pushed":1,"pulled":2,"rejected":3,"error":"e"}]}` {
		t.Fatalf("report json = %s", raw)
	}
}

func TestHLCStepsRefuseBeyondMaxWall(t *testing.T) {
	full := HLC{MaxHLCWall, math.MaxUint32, devB}
	tests := []struct {
		name string
		run  func() (HLC, error)
		ok   bool
	}{
		{"tick at max wall bumps counter", func() (HLC, error) { return TickHLC(HLC{MaxHLCWall, 1, devB}, 0, devA) }, true},
		{"tick saturated", func() (HLC, error) { return TickHLC(full, 0, devA) }, false},
		{"tick wall clock past max", func() (HLC, error) { return TickHLC(HLC{}, MaxHLCWall+1, devA) }, false},
		{"observe saturated remote", func() (HLC, error) { return ObserveHLC(HLC{}, full, 0, devA) }, false},
		{"observe saturated local", func() (HLC, error) { return ObserveHLC(full, HLC{}, 0, devA) }, false},
		{"observe remote wall past max", func() (HLC, error) { return ObserveHLC(HLC{}, HLC{Wall: MaxHLCWall + 1}, 0, devA) }, false},
		{"observe carry to max wall", func() (HLC, error) {
			return ObserveHLC(HLC{}, HLC{MaxHLCWall - 1, math.MaxUint32, devB}, 0, devA)
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			var ce *CodedError
			if tc.ok != (err == nil) || (!tc.ok && (!errors.As(err, &ce) || ce.Code != ErrValidation)) {
				t.Fatalf("got %+v, %v, want ok=%v", got, err, tc.ok)
			}
			if tc.ok {
				if _, perr := ParseHLC(got.String()); perr != nil {
					t.Fatalf("result does not round-trip: %v", perr)
				}
			}
		})
	}
}

func TestValidateSyncRemoteURL(t *testing.T) {
	tests := []struct {
		raw      string
		insecure bool
		ok       bool
	}{
		{"https://sync.example.com", false, true},
		{"http://localhost:8080", false, true},
		{"http://127.0.0.1:8080", false, true},
		{"http://127.9.9.9", false, true},
		{"http://[::1]:8080", false, true},
		{"http://example.com", false, false},
		{"http://10.0.0.1", false, false},
		{"http://localhost.example.com", false, false},
		{"http://example.com", true, true},
		{"ftp://example.com", true, false},
		{"https://", false, false},
		{"example.com", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			err := ValidateSyncRemoteURL(tc.raw, tc.insecure)
			if (err == nil) != tc.ok {
				t.Fatalf("insecure=%v: got %v, want ok=%v", tc.insecure, err, tc.ok)
			}
		})
	}
}

func TestValidateSyncProjectInfo(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SyncProjectInfo)
		ok     bool
	}{
		{"valid", func(*SyncProjectInfo) {}, true},
		{"multiline description", func(i *SyncProjectInfo) { i.Description = "a\n\tb" }, true},
		{"bad uid", func(i *SyncProjectInfo) { i.UID = "x" }, false},
		{"empty slug", func(i *SyncProjectInfo) { i.Slug = "" }, false},
		{"uppercase slug", func(i *SyncProjectInfo) { i.Slug = "My-Proj" }, false},
		{"slug with newline", func(i *SyncProjectInfo) { i.Slug = "a\nb" }, false},
		{"slug with path", func(i *SyncProjectInfo) { i.Slug = "../x" }, false},
		{"empty name", func(i *SyncProjectInfo) { i.Name = "" }, false},
		{"name with escape", func(i *SyncProjectInfo) { i.Name = "a\x1b[2Jb" }, false},
		{"name with newline", func(i *SyncProjectInfo) { i.Name = "a\nb" }, false},
		{"description with escape", func(i *SyncProjectInfo) { i.Description = "a\x1bb" }, false},
		{"kit key with tab", func(i *SyncProjectInfo) { i.KitKey = "a\tb" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := SyncProjectInfo{UID: devA, Name: "Proj", Slug: "proj", Description: "d", KitKey: "kit"}
			tc.mutate(&info)
			if err := ValidateSyncProjectInfo(info); (err == nil) != tc.ok {
				t.Fatalf("got %v, want ok=%v", err, tc.ok)
			}
		})
	}
}
