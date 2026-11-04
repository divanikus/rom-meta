//go:build !windows

package catalogfold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanIsolatesInaccessibleMedia(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged user for real permission checks")
	}
	for _, kind := range []string{"directory", "file", "held-directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			webPlanFixture(t, root)
			writeFoldGame(t, root, "Console", "GoodOne", "Garden (USA)", "Garden", "11")
			writeFoldGame(t, root, "Console", "GoodTwo", "Garden (Japan)", "Garden", "11")
			if kind == "held-directory" {
				writeFoldGame(t, root, "Console", "Two", "Orbit (Japan) (Special Edition)", "Orbit", "10")
			}
			path := filepath.Join(root, "platforms", "Console", "One", "media", "private")
			restore := os.FileMode(0755)
			if kind == "file" {
				path = filepath.Join(root, "platforms", "Console", "One", "media", "cover.jpg")
				writeFoldFile(t, path, "same media")
				writeFoldFile(t, filepath.Join(root, "platforms", "Console", "Two", "media", "cover.jpg"), "same media")
				restore = 0644
			} else {
				writeFoldFile(t, filepath.Join(path, "cover.jpg"), "private media")
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(path, restore)
			before := readFoldFile(t, filepath.Join(root, "platforms", "Console", "One", "game.yaml"))
			plan, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()
			if len(plan.Groups) != 2 {
				t.Fatal(plan.Groups)
			}
			var blocked PlannedGroup
			for _, pg := range plan.Groups {
				if pg.Group.Keep.Title == "Garden" {
					if pg.Status != "ready" {
						t.Fatal(pg)
					}
				} else {
					blocked = pg
					if pg.Status != "blocked" || pg.Result != nil || pg.inspectionErr == nil || !strings.Contains(pg.Reason, "permission denied") {
						t.Fatalf("unreadable group not clearly blocked: %+v", pg)
					}
				}
			}
			if err := os.Chmod(path, restore); err != nil {
				t.Fatal(err)
			}
			if _, err := plan.Preview(blocked.ID, Draft{Confirm: true, Reason: "Not an access override"}); err == nil {
				t.Fatal("incomplete inspection reused after access repair")
			}
			for _, action := range []string{"merge", "separate"} {
				if _, err := plan.Apply(context.Background(), []Decision{{ID: blocked.ID, Revision: 1, Action: action}}); err == nil {
					t.Fatal("unverified group allowed a write", action)
				}
			}
			if got := readFoldFile(t, filepath.Join(root, "platforms", "Console", "One", "game.yaml")); got != before {
				t.Fatal("blocked card changed")
			}
			if _, err := os.Stat(filepath.Join(root, "backups")); !os.IsNotExist(err) {
				t.Fatal("blocked write created staging", err)
			}
			fresh, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			for _, pg := range fresh.Groups {
				if pg.inspectionErr != nil {
					t.Fatal(pg)
				}
				if kind != "held-directory" && pg.Status != "ready" {
					t.Fatal(pg)
				}
				if kind == "held-directory" && pg.Group.Keep.Title == "Orbit" && pg.Status != "blocked" {
					t.Fatal("content guard bypassed", pg)
				}
			}
		})
	}
}
