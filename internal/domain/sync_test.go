package domain

import (
	"encoding/json"
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
			got := TickHLC(tc.last, tc.wall, devA)
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			if got.Wall < tc.last.Wall || (got.Wall == tc.last.Wall && got.Counter <= tc.last.Counter) {
				t.Fatalf("%+v not strictly after %+v", got, tc.last)
			}
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
			got := ObserveHLC(tc.last, tc.remote, tc.wall, devA)
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for _, prior := range []HLC{tc.last, tc.remote} {
				if got.Wall < prior.Wall || (got.Wall == prior.Wall && got.Counter <= prior.Counter) {
					t.Fatalf("%+v not strictly after %+v", got, prior)
				}
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

	report := SyncReport{Projects: []SyncProjectReport{{Slug: "s", Pushed: 1, Pulled: 2}}}
	raw, _ = json.Marshal(report)
	if string(raw) != `{"projects":[{"slug":"s","pushed":1,"pulled":2}]}` {
		t.Fatalf("report json = %s", raw)
	}
}
