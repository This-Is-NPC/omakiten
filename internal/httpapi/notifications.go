package httpapi

import (
	"net/http"
	"sort"

	"omakiten/internal/config"
)

// NotificationsResponse is every notification card a project's kits load,
// by kit key: the root kit and, when the project has one, its sub-task kit.
// A client shows a recorded notification.shown with the card its
// resolved_kit names, as the TUI shows one it raises itself.
type NotificationsResponse struct {
	Kits map[string][]config.Notification `json:"kits"`
}

func (s *Server) notificationRoute() route {
	return query("getNotifications", http.MethodGet, projectPath+"/notifications", "", "Notification cards of a project's kits: frames, size, colours, placement, and dismissal.", []param{projectParam}, s.notifications)
}

func (s *Server) notifications(r *http.Request) (NotificationsResponse, error) {
	snap, err := s.opts.Runtimes.Snapshot(r.Context(), r.PathValue("project"))
	if err != nil {
		return NotificationsResponse{}, err
	}
	out := NotificationsResponse{Kits: map[string][]config.Notification{snap.KitKey(): cardsOf(snap)}}
	if sub, ok := snap.SubtaskKit(); ok {
		out.Kits[sub.KitKey()] = cardsOf(sub)
	}
	return out, nil
}

// cardsOf lists a kit's notification cards by name.
func cardsOf(snap *config.Snapshot) []config.Notification {
	cards := make([]config.Notification, 0, len(snap.Notifications()))
	for _, card := range snap.Notifications() {
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].Name < cards[j].Name })
	return cards
}
