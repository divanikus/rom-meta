package sourceassets

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"sync"
	"testing"
)

func TestPackedConcurrentMediaDownloadsKeepAllMembers(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Example", "Game", "title: Game\n")
	platform := filepath.Join(root, "platforms", "Example")
	game := filepath.Join(platform, "Game")
	catalogfstest.Pack(t, platform)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "synthetic "+r.URL.Path) }))
	defer server.Close()
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("screenshot-%d.launchbox.png", i)
			errors <- downloadAsset(server.URL+"/"+n, server.URL, filepath.Join(game, "media", n), nil, n, 1, 4)
		}()
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		if e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 4; i++ {
		n := fmt.Sprintf("screenshot-%d.launchbox.png", i)
		b, e := catalogfs.ReadFile(filepath.Join(game, "media", n))
		if e != nil || string(b) != "synthetic /"+n {
			t.Fatal(n, string(b), e)
		}
		if _, e = os.Stat(filepath.Join(game, "media", n)); !os.IsNotExist(e) {
			t.Fatal("download left a loose member", e)
		}
	}
}
