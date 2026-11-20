//go:build darwin || linux

package cache

import "syscall"

// writable reports whether the user may create entries in dir. It asks the
// kernel, so ownership, groups and a read-only file system all count.
func writable(dir string) bool {
	const writeAndSearch = 0x2 | 0x1 // W_OK | X_OK
	return syscall.Access(dir, writeAndSearch) == nil
}
