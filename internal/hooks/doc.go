// Package hooks runs the YAML-driven hook engine that subscribes to the
// in-process events bus (internal/events) and dispatches matching domain
// events to configured actions (internal/hooks/actions). Each hook
// declares an event filter (`on`, `when`) and an action name; matched
// hooks execute on a goroutine bounded by the engine's WaitGroup so slow
// scripts cannot block the publisher.
package hooks
