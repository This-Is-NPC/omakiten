-- The TUI realtime refresh tick used to write one
-- `app.MetricsService.Summary` operation event per second while the user
-- dwelt on Stats › General. With the activity log capped at 500 rows
-- this could push every legitimate CLI / MCP entry off the bounded
-- window and break the Sources counter on Stats › Logs.
--
-- The fix landed in code via `activity.WithoutTracking` wrapping the
-- tick context, so no new pollution is written. This migration is the
-- one-shot cleanup of the historical rows that already accumulated in
-- existing databases. Trade-off: explicit `r`-triggered Summary calls
-- that happened to land in this same source/operation pair are also
-- purged — but those calls are read-only aggregates with no side
-- effects, so losing them costs nothing.

DELETE FROM events
WHERE event_type = 'operation'
  AND source = 'tui'
  AND operation = 'app.MetricsService.Summary';
