package exportclient

import (
	"context"
	"os"
	"path/filepath"
	"rommeta/internal/exporter"
	"testing"
)

func TestPrioritizedTitlesMatchLocalAndRemoteExports(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	payload := []byte("synthetic named-title payload")
	card(t, root, "n64", "Orbit", "Library title", "Orbit (USA)", "Orbit (USA).z64", payload)
	file := filepath.Join(root, "platforms", "n64", "Orbit", "game.yaml")
	data, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	data = append(data, []byte("sources:\n  manual.personal:\n    matches:\n      - title: Personal title\n  launchbox:\n    matches:\n      - title: Provider title\n")...)
	put(t, file, data)
	put(t, filepath.Join(roms, "Orbit (USA).z64"), payload)
	api := server(t, root, nil)
	for _, tc := range []struct{ sources, want string }{{"manual.personal, launchbox", "Personal title"}, {"launchbox, manual.personal", "Provider title"}, {"", "Orbit"}} {
		profile := filepath.Join(t.TempDir(), "profile.yaml")
		put(t, profile, []byte("selection:\n  fields:\n    title:\n      sources: ["+tc.sources+"]\n"))
		assertLocalParity(t, root, roms, "n64", api.URL, profile)
		job, e := Build(context.Background(), Options{API: api.URL, BatchOptions: exporter.BatchOptions{Options: exporter.Options{ROMRoot: roms, Platform: "n64", ProfilePath: profile, Out: filepath.Join(t.TempDir(), "out")}}})
		if e != nil {
			t.Fatal(e)
		}
		if got := job.Batch.Plans[0].Records[0].Fields["title"].Value; got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
