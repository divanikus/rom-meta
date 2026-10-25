package catalogfold

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestFoldRenameRetriesTemporaryPermissionFailure(t *testing.T) {
	calls := 0
	var waits []time.Duration
	err := retryFoldRename(func() error {
		calls++
		if calls < 3 {
			return &os.PathError{Op: "rename", Path: "platform", Err: os.ErrPermission}
		}
		return nil
	}, func(d time.Duration) { waits = append(waits, d) })
	if err != nil || calls != 3 || len(waits) != 2 || waits[0] != 100*time.Millisecond || waits[1] != 200*time.Millisecond {
		t.Fatalf("calls=%d waits=%v error=%v", calls, waits, err)
	}
}
func TestFoldRenameBoundsPermissionRetriesAndReturnsOtherErrors(t *testing.T) {
	for _, failure := range []error{os.ErrPermission, os.ErrNotExist} {
		calls, waits := 0, 0
		err := retryFoldRename(func() error { calls++; return failure }, func(time.Duration) { waits++ })
		want := 1
		if errors.Is(failure, os.ErrPermission) {
			want = 5
		}
		if !errors.Is(err, failure) || calls != want || waits != want-1 {
			t.Fatalf("calls=%d waits=%d error=%v", calls, waits, err)
		}
	}
}
