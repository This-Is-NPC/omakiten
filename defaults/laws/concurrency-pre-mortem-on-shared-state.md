---
name: Concurrency pre-mortem on shared state
severity: error
---
When a change introduces concurrent access to shared mutable state — a goroutine touching a map, a counter incremented from multiple handlers, a struct read while another path writes it — the author files a short concurrency pre-mortem before the code lands. It names the shared state, the access pattern (who reads, who writes, when), the race or deadlock it could produce, and the guard chosen (mutex, channel, atomic, immutability, confinement). Run the race detector and say so.

Bad: a request counter incremented with `c.n++` from many goroutines; `go test -race` was never run, and the metric silently undercounted under load.
Good: the pre-mortem named the shared counter, chose `atomic.Int64`, and the hand-back cites a green `go test -race ./...`.

See Goetz et al., *Java Concurrency in Practice*, on publication and confinement; and the Go memory model on data races and synchronisation.
