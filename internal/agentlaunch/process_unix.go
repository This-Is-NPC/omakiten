//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package agentlaunch

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts argv in a new session, so it outlives this process and
// its terminal.
func detach(argv, env []string, dir, output string) error {
	out, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env, cmd.Dir, cmd.Stdout, cmd.Stderr = env, dir, out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// harnessAttr puts a harness in its own process group, led by its pid.
func harnessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// inheritLock hands the session lock to the harness, so the lock stays
// held while any process of the run lives.
func inheritLock(cmd *exec.Cmd, lock *os.File) {
	cmd.ExtraFiles = []*os.File{lock}
}

// stopGroup asks every process of the group led by pid to stop.
func stopGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}
