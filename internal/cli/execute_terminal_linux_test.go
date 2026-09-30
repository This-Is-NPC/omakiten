package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestCLITerminalErrorKeepsStdoutMachineReadable(t *testing.T) {
	fixture := newCLIDBFixture(t, "database with spaces.db")
	var stdout bytes.Buffer
	transcript := driveTerminal(t, []terminalReply{{"okt task continue --help", "\n"}}, func(ctx context.Context) {
		cmd := NewRootCommand("test", Runners{})
		cmd.SetContext(ctx)
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"--db", fixture.dbPath, "--config", fixture.configPath, "task", "continue", "999999"})
		if Execute(cmd) != 1 {
			t.Fatal("missing task succeeded")
		}
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	})
	recovery := "okt --db " + shellQuoteArg(fixture.dbPath) + " --config " + fixture.configPath + " list"
	if !strings.Contains(transcript, "task_not_found") || !strings.Contains(transcript, recovery) {
		t.Fatalf("terminal recovery absent: %s", transcript)
	}
	if decodeEnvelope(t, stdout.String())["code"] != "task_not_found" {
		t.Fatalf("JSON output corrupted: %s", &stdout)
	}
}
