package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// openStream starts streamEvents on a live test server and returns a
// reader over its SSE lines.
func openStream(t *testing.T, server *Server, query string, lastEventID string) (*bufio.Reader, *http.Response) {
	t.Helper()
	live := httptest.NewServer(server)
	t.Cleanup(live.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, live.URL+"/api/v1/events?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := live.Client().Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return bufio.NewReader(resp.Body), resp
}

// nextMessage reads one SSE message, skipping comments, within a deadline.
func nextMessage(t *testing.T, reader *bufio.Reader) map[string]string {
	t.Helper()
	done := make(chan map[string]string, 1)
	go func() { done <- readMessage(reader) }()
	select {
	case message := <-done:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("no SSE message within 5s")
		return nil
	}
}

func readMessage(reader *bufio.Reader) map[string]string {
	message := map[string]string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return message
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "" && len(message) > 0:
			return message
		case line == "", strings.HasPrefix(line, ":"):
			continue
		}
		key, value, _ := strings.Cut(line, ": ")
		message[key] = value
	}
}

func waitForSubscriber(t *testing.T, hub *Hub) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		n := len(hub.subs)
		hub.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("stream never subscribed")
}

func TestStreamDeliversFilteredLiveRows(t *testing.T) {
	server, hub := newTestServer(t, &fakeOps{}, fakeLog{})
	reader, resp := openStream(t, server, "project=alpha&category=task", "")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	waitForSubscriber(t, hub)

	hub.Publish([]contract.LogsRow{
		{ID: 10, ProjectID: 8, EventType: "task.created", Category: "task"},
		{ID: 11, ProjectID: 7, EventType: "comment.added", Category: "comment"},
		{ID: 12, ProjectID: 7, EventType: "task.moved", Category: "task"},
	})
	message := nextMessage(t, reader)
	if message["id"] != "12" || message["event"] != "task.moved" || !strings.Contains(message["data"], `"project_id":7`) {
		t.Fatalf("message = %+v, want only project alpha task rows", message)
	}
}

func TestStreamReplaysAfterLastEventID(t *testing.T) {
	log := fakeLog{rows: []contract.LogsRow{
		{ID: 4, ProjectID: 7, EventType: "task.created", Category: "task"},
		{ID: 5, ProjectID: 7, EventType: "task.moved", Category: "task"},
	}}
	server, _ := newTestServer(t, &fakeOps{}, log)
	reader, _ := openStream(t, server, "project=alpha", "4")
	if message := nextMessage(t, reader); message["id"] != "5" {
		t.Fatalf("replay = %+v, want id 5", message)
	}
}

func TestHubSignalsOverflowWhenBufferFills(t *testing.T) {
	hub := NewHub()
	sub := hub.subscribe()
	defer hub.unsubscribe(sub)
	rows := make([]contract.LogsRow, subscriberBuffer+1)
	for i := range rows {
		rows[i] = contract.LogsRow{ID: int64(i + 1), ProjectID: 7}
	}
	hub.Publish(rows)
	select {
	case <-sub.overflow:
	default:
		t.Fatal("overflow not signalled after the buffer filled")
	}
}

func TestStreamRefusesProjectsWithoutLogsAccess(t *testing.T) {
	ops := &fakeOps{listLogs: func(contract.ListLogsInput) (contract.ListLogsResponse, error) {
		return contract.ListLogsResponse{}, domain.NewError(domain.ErrOperationDenied, "denied", nil)
	}}
	server, _ := newTestServer(t, ops, fakeLog{})
	for query, want := range map[string]int{
		"project=alpha":                http.StatusForbidden,
		"":                             http.StatusBadRequest,
		"project=ghost":                http.StatusNotFound,
		"project=alpha&category=bogus": http.StatusBadRequest,
	} {
		rec := do(t, server, http.MethodGet, "/api/v1/events?"+query, "", nil)
		if rec.Code != want {
			t.Errorf("query %q = %d %s, want %d", query, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestHubCloseEndsStreams(t *testing.T) {
	server, hub := newTestServer(t, &fakeOps{}, fakeLog{})
	reader, _ := openStream(t, server, "project=alpha", "")
	waitForSubscriber(t, hub)
	hub.Close()
	done := make(chan error, 1)
	go func() {
		_, err := reader.ReadString('\x00')
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream stayed open after Hub.Close")
	}
}
