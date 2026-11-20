//go:build !(darwin || linux)

package cache

// writable cannot be answered without writing on this system, so Check
// leaves the question to the first write.
func writable(string) bool { return true }
