package openvgdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotDownloadHonorsDeadlineAndPreservesExistingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "snapshot.zip")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 50 * time.Millisecond}
	err := downloadFileWithClient(client, server.URL, path, "download", "test", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "original" {
		t.Fatalf("original lost: %q, %v", contents, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("partial file left: %v", err)
	}
}
