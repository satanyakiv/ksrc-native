//go:build !unix

package executil

import "os/exec"

// ownProcessGroup does nothing on a system without process groups: a cancel
// kills the program itself, and the wait delay of Run bounds the rest.
func ownProcessGroup(*exec.Cmd) {}
