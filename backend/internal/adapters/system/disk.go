package system

import "syscall"

// FreeBytes returns how many bytes the backend may still write to the filesystem holding path.
func FreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	// Bavail rather than Bfree: blocks reserved for root aren't the backend's to use.
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:gosec // block counts and sizes fit int64
}
