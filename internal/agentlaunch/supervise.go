package agentlaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// errTimedOut is the cause of a run its own timeout ended.
var errTimedOut = errors.New("the run timed out")

// stopGrace is how long a harness has to exit after its stop signal before
// it is killed.
const stopGrace = 10 * time.Second

// Result is how a supervised harness ended: its exit code, and whether the
// timeout or a stop request ended it.
type Result struct {
	ExitCode int
	TimedOut bool
	Stopped  bool
}

// Run is one supervised harness run: its argv, the transcript its output
// goes to, the timeout that ends it, zero for none, the session lock the
// harness inherits, and Started, called with the harness pid once it runs.
type Run struct {
	Argv       []string
	Transcript string
	Timeout    time.Duration
	Lock       *Lock
	Started    func(pid int)
}

// Supervise runs the harness in its own process group until it exits,
// appending its output to the transcript as JSONL: a stdout line that is a
// JSON object is kept as is, any other line becomes {"stream": ...,
// "text": ...}. The timeout and ctx's end stop the group. The error reports
// a harness that could not start.
func Supervise(ctx context.Context, run Run) (Result, error) {
	if len(run.Argv) == 0 {
		return Result{}, errors.New("the harness argv is empty")
	}
	out, err := os.OpenFile(run.Transcript, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = out.Close() }()
	runCtx := ctx
	if run.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeoutCause(ctx, run.Timeout, errTimedOut)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, run.Argv[0], run.Argv[1:]...)
	cmd.SysProcAttr = harnessAttr()
	cmd.Cancel = func() error { return stopGroup(cmd.Process.Pid) }
	cmd.WaitDelay = stopGrace
	if run.Lock != nil {
		inheritLock(cmd, run.Lock.file)
	}
	w := &transcriptWriter{out: out}
	stdout, stderr := &streamWriter{w: w, stream: "stdout"}, &streamWriter{w: w, stream: "stderr"}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	if run.Started != nil {
		run.Started(cmd.Process.Pid)
	}
	_ = cmd.Wait()
	stdout.flush()
	stderr.flush()
	result := Result{ExitCode: cmd.ProcessState.ExitCode()}
	switch {
	case context.Cause(runCtx) == errTimedOut:
		result.TimedOut = true
	case ctx.Err() != nil:
		result.Stopped = true
	}
	return result, nil
}

// transcriptWriter appends whole lines from several streams to one file.
type transcriptWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *transcriptWriter) write(stream string, line []byte) {
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return
	}
	if stream != "stdout" || line[0] != '{' || !json.Valid(line) {
		line, _ = json.Marshal(map[string]string{"stream": stream, "text": string(line)})
	}
	record := append(make([]byte, 0, len(line)+1), line...)
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.out.Write(append(record, '\n'))
}

// streamWriter splits one stream into lines for the transcript.
type streamWriter struct {
	w      *transcriptWriter
	stream string
	buf    []byte
}

func (s *streamWriter) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		s.w.write(s.stream, s.buf[:i+1])
		s.buf = s.buf[i+1:]
	}
}

// flush writes a last line the stream did not end.
func (s *streamWriter) flush() {
	s.w.write(s.stream, s.buf)
	s.buf = nil
}
