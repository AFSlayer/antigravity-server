//go:build !linux && !darwin

package customplugins

import "os"

// fileIdentity gives nothing more where the system's file information is not read: the size and the modification
// time stand alone.
func fileIdentity(os.FileInfo) (uint64, int64) { return 0, 0 }
