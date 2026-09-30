package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/httpapi"
)

const shutdownTimeout = 5 * time.Second

// Run serves the local HTTP API until ctx ends or the process receives
// SIGINT or SIGTERM, then drains requests and removes its published files.
func Run(ctx context.Context, session agentruntime.Session, opts contract.ServeOptions) (runErr error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	catalog := session.Snapshot.Catalog(config.SurfaceCLI)

	listener, err := listenLoopback(opts.Addr, catalog)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	instance, err := claimInstance(catalog)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, instance.release()) }()

	return serve(ctx, listener, instance, session, opts, catalog)
}

// serve publishes the discovery files, runs the server and the event feed,
// and drains both when ctx ends.
func serve(ctx context.Context, listener net.Listener, instance *instance, session agentruntime.Session, opts contract.ServeOptions, catalog *config.Catalog) error {
	url := "http://" + listener.Addr().String()
	token, err := instance.publish(url, session.Version)
	if err != nil {
		return err
	}
	hub := httpapi.NewHub()
	feed, err := newFeed(ctx, session.Store, hub)
	if err != nil {
		return err
	}
	handler, err := httpapi.New(httpapi.Options{
		Token:    token,
		Version:  session.Version,
		Runtimes: runtimes{session: session},
		Hub:      hub,
		Log:      feed,
	})
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	go feed.run(ctx, opts.Poll)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	if opts.Stderr != nil {
		_, _ = fmt.Fprintf(opts.Stderr, catalog.Get("cli.serve.listening_fmt")+"\n", url, instance.DiscoveryPath())
	}

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	hub.Close()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func listenLoopback(addr string, catalog *config.Catalog) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, domain.NewError(domain.ErrValidation, err.Error(), map[string]any{"addr": addr})
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, domain.NewError(domain.ErrValidation, catalog.Get("cli.serve.error.not_loopback"), map[string]any{"addr": addr})
	}
	return net.Listen("tcp", addr)
}
