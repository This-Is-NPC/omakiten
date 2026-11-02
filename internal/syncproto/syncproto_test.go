package syncproto

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

const dev domain.UID = "01900000-0000-7000-8000-00000000000a"

func validChange() domain.SyncChange {
	return domain.SyncChange{
		Table:  "tasks",
		RowUID: "01900000-0000-7000-8000-0000000000aa",
		Op:     domain.SyncOpUpsert,
		HLC:    domain.HLC{Wall: 1700000000000, Device: dev}.String(),
		Device: dev,
		Data:   map[string]any{"title": "t"},
	}
}

func TestRoutes(t *testing.T) {
	if got := ChangesPath(dev); got != "/v1/projects/"+string(dev)+"/changes" {
		t.Fatalf("ChangesPath = %s", got)
	}
	if got := PullQuery(5, 100); got != "after=5&limit=100" {
		t.Fatalf("PullQuery = %s", got)
	}
}

func TestBodiesRoundTrip(t *testing.T) {
	info := domain.SyncProjectInfo{UID: dev, Name: "N", Slug: "n", Description: "d", KitKey: "k"}
	bodies := []struct {
		in  any
		out any
		raw string
	}{
		{ProjectsResponse{Projects: []domain.SyncProjectInfo{info}}, new(ProjectsResponse), `"projects"`},
		{PushRequest{Project: info, Changes: []domain.SyncChange{validChange()}}, new(PushRequest), `"changes"`},
		{PushResponse{Accepted: 2, Rejected: 1, Cursor: 9}, new(PushResponse), `{"accepted":2,"rejected":1,"cursor":9}`},
		{PullResponse{Project: info, Changes: []domain.SyncChange{validChange()}, Cursor: 3, More: true}, new(PullResponse), `"more":true`},
	}
	for _, b := range bodies {
		raw, err := json.Marshal(b.in)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(raw) || !strings.Contains(string(raw), b.raw) {
			t.Fatalf("json %s lacks %s", raw, b.raw)
		}
		if err := json.Unmarshal(raw, b.out); err != nil {
			t.Fatal(err)
		}
		if got := reflect.ValueOf(b.out).Elem().Interface(); !reflect.DeepEqual(got, b.in) {
			t.Fatalf("round trip = %+v, want %+v", got, b.in)
		}
	}
}
