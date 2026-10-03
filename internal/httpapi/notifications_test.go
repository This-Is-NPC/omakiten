package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"omakiten/internal/config"
)

func TestNotificationsServeTheCardsOfEachKit(t *testing.T) {
	root := studioBundle(nil)
	root.Notifications = map[string]config.Notification{
		"kitten_success": {Name: "kitten_success", FrameIntervalMs: 500, Animation: []config.NotificationFrame{{Frame: 0, Value: "=^.^="}}},
		"kitten_blocked": {Name: "kitten_blocked", Border: config.NotificationBorder{Color: "$theme.warning"}},
	}
	sub := studioBundle(nil)
	sub.Kit = config.Kit{Key: "izakaya"}
	sub.Notifications = map[string]config.Notification{"kitten_blocked": {Name: "kitten_blocked"}}
	root.SubtaskBundle = &sub
	server := newStudioServer(t, fakeRuntimes{projects: map[string]int64{"alpha": 7}, snapshot: config.BuildSnapshot(root)})

	rec := do(t, server, http.MethodGet, "/api/v1/projects/alpha/notifications", "", nil)
	env := decode(t, rec)
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("notifications = %d %s", rec.Code, rec.Body.String())
	}
	var got NotificationsResponse
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	cards := got.Kits["omakase"]
	if len(cards) != 2 || cards[0].Name != "kitten_blocked" || cards[0].Border.Color != "$theme.warning" || cards[1].Animation[0].Value != "=^.^=" {
		t.Fatalf("omakase cards = %+v, want both by name with their frames and colours", cards)
	}
	if len(got.Kits["izakaya"]) != 1 {
		t.Fatalf("kits = %v, want the sub-task kit's cards too", got.Kits)
	}
}
