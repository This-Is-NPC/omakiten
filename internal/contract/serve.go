package contract

import (
	"io"
	"time"
)

// ServeOptions configures the local HTTP daemon.
type ServeOptions struct {
	// Addr is the loopback host:port to listen on; port 0 picks a free port.
	Addr string
	// Poll is the interval between checks for events committed by any process.
	Poll time.Duration
	// Stderr receives the startup line.
	Stderr io.Writer
}
