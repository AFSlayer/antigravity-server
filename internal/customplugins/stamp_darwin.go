package customplugins

import (
	"os"
	"syscall"
)

// fileIdentity gives a file's number and the time its inode last changed.
func fileIdentity(info os.FileInfo) (uint64, int64) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino), st.Ctimespec.Nano()
	}
	return 0, 0
}
