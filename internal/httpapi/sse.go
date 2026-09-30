package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

const (
	subscriberBuffer = 256
	// replayLimit bounds Last-Event-ID replay; a longer gap asks the
	// client to resync from the list routes.
	replayLimit = 1000
)

// Hub fans committed event rows out to SSE subscribers. A subscriber that
// falls behind its buffer is told to resync and disconnected.
type Hub struct {
	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	closed chan struct{}
	once   sync.Once
}

type subscriber struct {
	rows     chan contract.LogsRow
	overflow chan struct{}
	once     sync.Once
}

func NewHub() *Hub {
	return &Hub{subs: map[*subscriber]struct{}{}, closed: make(chan struct{})}
}

// Close ends every stream so a server shutdown does not wait on them.
func (h *Hub) Close() {
	h.once.Do(func() { close(h.closed) })
}

// Publish delivers rows, oldest first, to every subscriber without blocking.
func (h *Hub) Publish(rows []contract.LogsRow) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		for _, row := range rows {
			select {
			case sub.rows <- row:
			default:
				sub.once.Do(func() { close(sub.overflow) })
			}
		}
	}
}

func (h *Hub) subscribe() *subscriber {
	sub := &subscriber{rows: make(chan contract.LogsRow, subscriberBuffer), overflow: make(chan struct{})}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *Hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

// streamFilter keeps the rows of the authorized projects and categories.
type streamFilter struct {
	projects   map[int64]struct{}
	categories []string
}

func (f streamFilter) match(row contract.LogsRow) bool {
	if _, ok := f.projects[row.ProjectID]; !ok {
		return false
	}
	return len(f.categories) == 0 || slices.Contains(f.categories, row.Category)
}

func (s *Server) eventsRoute() route {
	return route{
		id:      "streamEvents",
		method:  http.MethodGet,
		path:    "/api/v1/events",
		slug:    "logs.list",
		summary: "Server-sent stream of committed events. Each message id is the event id; send Last-Event-ID to resume. An `event: resync` message asks the client to reload its lists.",
		params: []param{
			{name: "project", in: "query", description: "Project slug; repeatable or comma-separated.", schema: listSchema, required: true},
			queryParam("category", "Event category; repeatable or comma-separated.", listSchema),
		},
		result: reflect.TypeFor[contract.LogsRow](),
		stream: s.streamEvents,
	}
}

// streamEvents authorizes logs.list on every requested project, replays
// the gap after Last-Event-ID, then forwards live rows.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, fmt.Errorf("response writer cannot stream"))
		return
	}
	filter, err := s.streamFilter(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	cursor, err := lastEventID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}

	sub := s.opts.Hub.subscribe()
	defer s.opts.Hub.unsubscribe(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if cursor > 0 {
		if cursor, ok = s.replay(r.Context(), w, filter, cursor); !ok {
			writeResync(w, flusher)
			return
		}
		flusher.Flush()
	}
	s.forward(r.Context(), w, flusher, sub, filter, cursor)
}

// replay writes the rows committed after cursor. ok is false when the gap
// exceeds replayLimit or cannot be read.
func (s *Server) replay(ctx context.Context, w http.ResponseWriter, filter streamFilter, cursor int64) (int64, bool) {
	rows, err := s.opts.Log.EventsAfter(ctx, cursor, replayLimit+1)
	if err != nil || len(rows) > replayLimit {
		return cursor, false
	}
	for _, row := range rows {
		if filter.match(row) {
			writeRow(w, row)
		}
		cursor = row.ID
	}
	return cursor, true
}

// forward writes live rows newer than cursor until the client leaves, the
// hub closes, or the subscriber overflows.
func (s *Server) forward(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, sub *subscriber, filter streamFilter, cursor int64) {
	heartbeat := time.NewTicker(s.opts.Heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.opts.Hub.closed:
			return
		case <-sub.overflow:
			writeResync(w, flusher)
			return
		case row := <-sub.rows:
			if row.ID <= cursor {
				continue
			}
			cursor = row.ID
			if filter.match(row) {
				writeRow(w, row)
				flusher.Flush()
			}
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) streamFilter(r *http.Request) (streamFilter, error) {
	slugs := listValues(r, "project")
	if len(slugs) == 0 {
		return streamFilter{}, invalidParameter("project", "")
	}
	filter := streamFilter{projects: map[int64]struct{}{}, categories: listValues(r, "category")}
	for _, category := range filter.categories {
		if !slices.Contains(domain.KnownEventCategories, domain.EventCategory(category)) {
			return streamFilter{}, invalidParameter("category", category)
		}
	}
	for _, slug := range slugs {
		ops, selector, err := s.opts.Runtimes.Project(r.Context(), slug)
		if err != nil {
			return streamFilter{}, err
		}
		if err := authorizeLogs(r.Context(), ops, selector); err != nil {
			return streamFilter{}, err
		}
		filter.projects[selector.ProjectID] = struct{}{}
	}
	return filter, nil
}

// authorizeLogs runs a one-row logs.list so the surface table and project
// resolution decide stream access exactly as they decide the list route.
func authorizeLogs(ctx context.Context, ops Operations, selector contract.ProjectSelector) error {
	_, err := ops.ListLogs(ctx, contract.ListLogsInput{ProjectSelector: selector, Limit: 1})
	return err
}

func lastEventID(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, invalidParameter("Last-Event-ID", raw)
	}
	return id, nil
}

func writeRow(w http.ResponseWriter, row contract.LogsRow) {
	data, err := json.Marshal(row)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", row.ID, row.EventType, data)
}

func writeResync(w http.ResponseWriter, flusher http.Flusher) {
	_, _ = fmt.Fprint(w, "event: resync\ndata: {}\n\n")
	flusher.Flush()
}
