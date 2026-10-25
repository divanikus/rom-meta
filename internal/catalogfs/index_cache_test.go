package catalogfs

import (
	"container/list"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockedMember struct {
	io.Reader
	started, unblock chan struct{}
	once             sync.Once
}

func (r *blockedMember) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.unblock })
	return r.Reader.Read(p)
}
func (r *blockedMember) Close() error { return nil }

type countedMember struct {
	io.Reader
	reads atomic.Int32
}

func (r *countedMember) Read(p []byte) (int, error) { r.reads.Add(1); return r.Reader.Read(p) }
func (r *countedMember) Close() error               { return nil }

func TestMemberCacheIndependentLoadsAndSingleFlight(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	openArchive := func() *os.File {
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		return f
	}
	blocked := &blockedMember{Reader: strings.NewReader("data"), started: make(chan struct{}), unblock: make(chan struct{})}
	var unblock sync.Once
	defer unblock.Do(func() { close(blocked.unblock) })
	type result struct {
		f   *File
		err error
	}
	first := make(chan result, 1)
	go func() { f, e := cachedMember(path, info, blocked, openArchive()); first <- result{f, e} }()
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first load did not start")
	}
	duplicateReader := &countedMember{Reader: strings.NewReader("data")}
	duplicate := make(chan result, 1)
	archive := openArchive()
	go func() { f, e := cachedMember(path, info, duplicateReader, archive); duplicate <- result{f, e} }()
	other := make(chan result, 1)
	otherArchive := openArchive()
	go func() {
		f, e := cachedMember(path+"other", info, io.NopCloser(strings.NewReader("data")), otherArchive)
		other <- result{f, e}
	}()
	select {
	case r := <-other:
		if r.err != nil {
			t.Fatal(r.err)
		}
		r.f.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("unrelated member blocked behind decompression")
	}
	unblock.Do(func() { close(blocked.unblock) })
	a, b := <-first, <-duplicate
	if a.err != nil || b.err != nil {
		t.Fatal(a.err, b.err)
	}
	defer a.f.Close()
	defer b.f.Close()
	if a.f.Name() != b.f.Name() || duplicateReader.reads.Load() != 0 {
		t.Fatal("duplicate was inflated twice")
	}
}
func TestZIPInventoryLRUEvictsOnlyOldEntries(t *testing.T) {
	c := zipIndexes{entries: map[string]*list.Element{}}
	for i := 0; i < 4096; i++ {
		c.Store(string(rune(i+1)), &gameIndex{})
	}
	_, _ = c.Load(string(rune(1)))
	c.Store("new", &gameIndex{})
	if _, ok := c.Load(string(rune(1))); !ok {
		t.Fatal("hot entry discarded")
	}
	if _, ok := c.Load(string(rune(2))); ok {
		t.Fatal("oldest entry retained")
	}
	if _, ok := c.Load(string(rune(4096))); !ok {
		t.Fatal("whole cache flushed")
	}
}
