//go:build unix

package executil

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ownProcessGroup starts the program in a new process group and makes a
// cancel kill that group. The group id is the pid of the program, and every
// process it starts inherits the group unless it leaves on purpose.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
