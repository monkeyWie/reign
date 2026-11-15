//go:build !windows

package system

import "os"

func isExecutable(st os.FileInfo) bool { return st.Mode()&0111 != 0 }
