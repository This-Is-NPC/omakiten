package daemon

import (
	"context"
	"time"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/httpapi"
	"omakiten/internal/operation"
	"omakiten/internal/sqlite"
)

// feedBatch bounds one event-log read while draining.
const feedBatch = 500

// feed publishes committed events to the hub. It watches PRAGMA
// data_version, which moves when any other connection — another okt
// process or this daemon's own writers — commits, so every writer reaches
// SSE through the one event-log read.
type feed struct {
	store  *sqlite.Store
	hub    *httpapi.Hub
	cursor int64
}

func newFeed(ctx context.Context, store *sqlite.Store, hub *httpapi.Hub) (*feed, error) {
	cursor, err := store.LatestEventID(ctx)
	if err != nil {
		return nil, err
	}
	return &feed{store: store, hub: hub, cursor: cursor}, nil
}

// EventsAfter implements httpapi.EventLog.
func (f *feed) EventsAfter(ctx context.Context, afterID int64, limit int) ([]contract.LogsRow, error) {
	rows, err := f.store.ListEvents(ctx, domain.EventFilter{AfterID: afterID, Limit: limit})
	if err != nil {
		return nil, err
	}
	return visibleRows(rows), nil
}

func (f *feed) run(ctx context.Context, poll time.Duration) {
	version, _ := f.store.DataVersion(ctx)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := f.store.DataVersion(ctx)
			if err != nil || next == version {
				continue
			}
			version = next
			f.drain(ctx)
		}
	}
}

func (f *feed) drain(ctx context.Context) {
	for {
		rows, err := f.store.ListEvents(ctx, domain.EventFilter{AfterID: f.cursor, Limit: feedBatch})
		if err != nil || len(rows) == 0 {
			return
		}
		f.cursor = rows[len(rows)-1].ID
		if visible := visibleRows(rows); len(visible) > 0 {
			f.hub.Publish(visible)
		}
		if len(rows) < feedBatch {
			return
		}
	}
}

func visibleRows(rows []domain.EventRow) []contract.LogsRow {
	out := make([]contract.LogsRow, 0, len(rows))
	for _, row := range rows {
		if !row.LogHidden {
			out = append(out, operation.LogsRow(row))
		}
	}
	return out
}
