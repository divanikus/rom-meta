//go:build !linux

package catalogfs

import "os"

func packAttributes(file *os.File, before os.FileInfo) error {
	mode := os.FileMode(0644)
	if before != nil {
		mode = before.Mode().Perm()
	}
	return file.Chmod(mode)
}
