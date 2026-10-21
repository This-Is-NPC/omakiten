//go:build windows

package agentlaunch

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// detach starts argv without a console in a new process group, so it
// outlives this process.
func detach(argv, env []string, dir, output string) error {
	out, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env, cmd.Dir, cmd.Stdout, cmd.Stderr = env, dir, out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// harnessAttr puts a harness in its own process group.
func harnessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// inheritLock does nothing: Windows hands no extra files to a child, so
// the lock lives with the supervisor alone.
func inheritLock(*exec.Cmd, *os.File) {}

// stopGroup ends the process tree rooted at pid.
func stopGroup(pid int) error {
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}
