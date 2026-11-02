package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"omakiten/internal/syncserver"
)

var version = "dev"

const tokenEnv = "OKT_SERVER_TOKEN"

// minTokenBytes is the shortest bearer token the server accepts.
const minTokenBytes = 32

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "okt-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("okt-server", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:8787", "address to listen on")
	dbPath := flags.String("db", "", "path of the SQLite store (required)")
	tokenFile := flags.String("token-file", "", "file with one bearer token per line; "+tokenEnv+" is used when unset")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *dbPath == "" {
		return errors.New("--db is required")
	}
	tokens, err := loadTokens(*tokenFile, os.Getenv(tokenEnv))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := syncserver.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	server := &http.Server{
		Addr:              *listen,
		Handler:           syncserver.NewHandler(store, tokens),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	logger.Info("okt-server listening", "addr", *listen, "version", version)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	logger.Info("okt-server shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

// loadTokens reads tokens from path, or from fallback when path is empty, and
// rejects any shorter than minTokenBytes.
func loadTokens(path, fallback string) ([]string, error) {
	var tokens []string
	if path == "" {
		if t := strings.TrimSpace(fallback); t != "" {
			tokens = append(tokens, t)
		}
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				tokens = append(tokens, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	if len(tokens) == 0 {
		return nil, errors.New("no token: set --token-file or " + tokenEnv)
	}
	for i, t := range tokens {
		if len(t) < minTokenBytes {
			return nil, fmt.Errorf("token %d is shorter than the %d-byte minimum", i+1, minTokenBytes)
		}
	}
	return tokens, nil
}
