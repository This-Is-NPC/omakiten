//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || windows)

package agentlaunch

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

var errUnsupported = errors.New("headless agents are not supported on this platform")

func detach([]string, []string, string, string) error { return errUnsupported }

func harnessAttr() *syscall.SysProcAttr { return nil }

func inheritLock(*exec.Cmd, *os.File) {}

func stopGroup(int) error { return errUnsupported }
