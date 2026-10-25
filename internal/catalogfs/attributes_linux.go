package catalogfs

import (
	"os"
	"syscall"
)

func packAttributes(file *os.File, before os.FileInfo) error {
	mode := os.FileMode(0644)
	if before != nil {
		mode = before.Mode().Perm()
		if owner, ok := before.Sys().(*syscall.Stat_t); ok {
			now, e := file.Stat()
			if e != nil {
				return e
			}
			if actual, ok := now.Sys().(*syscall.Stat_t); ok && (actual.Uid != owner.Uid || actual.Gid != owner.Gid) {
				if e = file.Chown(int(owner.Uid), int(owner.Gid)); e != nil {
					return e
				}
			}
		}
	}
	return file.Chmod(mode)
}
