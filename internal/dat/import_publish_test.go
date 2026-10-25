package dat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublishImportedPlatformKeepsRecoverableData(t *testing.T) {
	for _, tc := range []struct {
		name         string
		existing     bool
		failCalls    map[int]bool
		wantCalls    int
		wantOriginal bool
		wantStage    bool
		wantBackup   bool
		message      string
	}{
		{name: "create", wantCalls: 1},
		{name: "merge", existing: true, wantCalls: 2},
		{name: "create activation failure", failCalls: map[int]bool{1: true}, wantCalls: 1, wantStage: true, message: "activate prepared platform"},
		{name: "backup failure", existing: true, failCalls: map[int]bool{1: true}, wantCalls: 1, wantOriginal: true, wantStage: true, message: "move existing platform to backup"},
		{name: "activation failure restores original", existing: true, failCalls: map[int]bool{2: true}, wantCalls: 3, wantOriginal: true, wantStage: true, message: "original platform restored"},
		{name: "rollback failure keeps both copies", existing: true, failCalls: map[int]bool{2: true, 3: true}, wantCalls: 3, wantStage: true, wantBackup: true, message: "restore original platform from"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			platform := filepath.Join(t.TempDir(), "platforms", "Arcade")
			writeContents := func(dir, contents string) {
				t.Helper()
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			checkContents := func(dir, want string) {
				t.Helper()
				if got := readText(t, filepath.Join(dir, "game.yaml")); got != want {
					t.Fatalf("%s = %q, want %q", dir, got, want)
				}
			}
			if tc.existing {
				writeContents(platform, "original curation")
			}
			var stage, backup string
			calls := 0
			writes, err := publishImportedPlatform(platform, func(path string) (int, error) {
				stage = path
				writeContents(path, "complete imported data")
				return 1, nil
			}, func(from, to string) error {
				calls++
				if from == platform {
					backup = to
				}
				if tc.failCalls[calls] {
					return &os.LinkError{Op: "rename", Old: from, New: to, Err: os.ErrPermission}
				}
				return os.Rename(from, to)
			})
			if calls != tc.wantCalls {
				t.Fatalf("rename calls = %d, want %d", calls, tc.wantCalls)
			}
			if tc.message == "" {
				if err != nil || writes != 1 {
					t.Fatalf("writes=%d err=%v", writes, err)
				}
				checkContents(platform, "complete imported data")
			} else {
				if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), tc.message) || writes != 0 {
					t.Fatalf("writes=%d err=%v", writes, err)
				}
				if !strings.Contains(err.Error(), "prepared platform retained at "+stage) {
					t.Fatalf("missing recovery path: %v", err)
				}
				if tc.wantOriginal {
					checkContents(platform, "original curation")
				} else {
					mustNotExist(t, platform)
				}
			}
			if tc.wantStage {
				checkContents(stage, "complete imported data")
			} else {
				mustNotExist(t, stage)
			}
			if tc.wantBackup {
				checkContents(backup, "original curation")
				if !strings.Contains(err.Error(), "restore original platform from "+backup) {
					t.Fatalf("missing backup recovery path: %v", err)
				}
			} else if backup != "" {
				mustNotExist(t, backup)
			}
		})
	}
}

func TestPublishImportedPlatformCleansIncompleteStage(t *testing.T) {
	platform := filepath.Join(t.TempDir(), "platforms", "Arcade")
	if err := os.MkdirAll(platform, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(platform, "game.yaml")
	if err := os.WriteFile(original, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeErr := errors.New("incomplete write")
	var stage string
	_, err := publishImportedPlatform(platform, func(path string) (int, error) {
		stage = path
		if err := os.WriteFile(filepath.Join(path, "partial.yaml"), []byte("partial"), 0o644); err != nil {
			t.Fatal(err)
		}
		return 0, writeErr
	}, func(string, string) error {
		t.Fatal("must not activate an incomplete stage")
		return nil
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("error = %v", err)
	}
	mustNotExist(t, stage)
	if got := readText(t, original); got != "original" {
		t.Fatalf("original changed: %q", got)
	}
}

func TestPublishedPlatformPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions")
	}
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			platform := filepath.Join(t.TempDir(), "platforms", "Example")
			want := os.FileMode(0755)
			if existing {
				want = 0750
				if err := os.MkdirAll(platform, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(platform, want); err != nil {
					t.Fatal(err)
				}
			}
			_, err := publishImportedPlatform(platform, func(stage string) (int, error) {
				info, err := os.Stat(stage)
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatal("stage not private", info, err)
				}
				return 0, nil
			}, os.Rename)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(platform)
			if err != nil || info.Mode().Perm() != want {
				t.Fatal("published permissions", info, err)
			}
		})
	}
}
