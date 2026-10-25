package catalogfold

import (
	"os"
	"time"

	"rommeta/internal/catalogfs"
)

func renameFoldDirectory(from, to string) error {
	return retryFoldRename(func() error { return catalogfs.Rename(from, to) }, time.Sleep)
}

// Windows/SMB can briefly deny a directory rename after its old name is moved.
// Bound the wait; a persistent permission failure still triggers normal rollback.
func retryFoldRename(rename func() error, wait func(time.Duration)) error {
	for attempt := 0; ; attempt++ {
		err := rename()
		if err == nil || !os.IsPermission(err) || attempt == 4 {
			return err
		}
		wait(100 * time.Millisecond << attempt)
	}
}
