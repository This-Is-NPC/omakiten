package cli

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"omakiten/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type terminalReply struct {
	prompt string
	keys   string
}

func openTestTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}

func driveTerminal(t *testing.T, replies []terminalReply, run func(context.Context)) string {
	t.Helper()
	master, slave := openTestTerminal(t)
	stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = slave, slave, slave
	defer func() { os.Stdin, os.Stdout, os.Stderr = stdin, stdout, stderr }()
	t.Setenv("TERM", "xterm")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stop := time.AfterFunc(10*time.Second, func() { _ = slave.Close(); _ = master.Close() })
	defer stop.Stop()
	type result struct {
		output  string
		pending int
	}
	finished := make(chan result, 1)
	go func() {
		output, pending := replyToTerminal(master, replies)
		finished <- result{output, pending}
	}()
	run(ctx)
	_ = slave.Close()
	_ = master.Close()
	outcome := <-finished
	if outcome.pending != 0 {
		t.Fatalf("terminal left %d unanswered prompts: %s", outcome.pending, outcome.output)
	}
	return outcome.output
}

func replyToTerminal(master *os.File, replies []terminalReply) (string, int) {
	var output strings.Builder
	buffer := make([]byte, 4096)
	consumed := 0
	for {
		n, err := master.Read(buffer)
		output.Write(buffer[:n])
		if err != nil {
			return output.String(), len(replies)
		}
		if len(replies) == 0 {
			continue
		}
		if index := strings.Index(output.String()[consumed:], replies[0].prompt); index >= 0 {
			consumed += index + len(replies[0].prompt)
			if _, err := master.Write([]byte(replies[0].keys)); err != nil {
				return output.String(), len(replies)
			}
			replies = replies[1:]
		}
	}
}

func TestCLIInteractiveConfigInitValidatesAndPersistsLanguages(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	cfg := filepath.Join(root, "config", "omakase.yaml")
	replies := []terminalReply{{"CLI language [", "unavailable\nen\npt-br\nPortuguês\n"}}
	var stdout bytes.Buffer
	driveTerminal(t, replies, func(context.Context) {
		cmd := NewRootCommand("test")
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"--db", filepath.Join(root, "state.db"), "--config", cfg, "config", "init", "--scope", "global", "--preset", "omakase"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	if decodeEnvelope(t, stdout.String())["ok"] != true {
		t.Fatalf("interactive JSON output corrupted: %s", &stdout)
	}
	bundle, err := config.LoadBundle(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := config.LanguageSettings{CLI: "en", TUI: "pt-br", AgentOutput: "Português"}
	if bundle.Config.Languages != want {
		t.Fatalf("interactive selection lost: %+v", bundle.Config.Languages)
	}
}
