package catalogfs

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const memberCacheBytes int64 = 1 << 30
const memberCacheEntries = 8

type cachedEntry struct {
	name string
	info os.FileInfo
	refs int
	used time.Time
}

var memberCache = struct {
	sync.Mutex
	entries map[string]*cachedEntry
}{entries: map[string]*cachedEntry{}}

var memberLoads = map[string]chan struct{}{} // protected by memberCache
var memberSlots = make(chan struct{}, 4)

// This disposable process-local cache keeps only selected deflated members, at
// most 1 GiB / eight closed entries. Active readers stay pinned until Close.
func cachedMember(key string, info os.FileInfo, r io.ReadCloser, archive *os.File) (*File, error) {
	for {
		memberCache.Lock()
		if old := memberCache.entries[key]; old != nil && SameFile(old.info, info) {
			f, e := os.Open(old.name)
			if e == nil {
				r.Close()
				archive.Close()
				old.refs++
				old.used = time.Now()
				memberCache.Unlock()
				return &File{File: f, info: info, release: releaseMember(old)}, nil
			}
		}
		if pending := memberLoads[key]; pending != nil {
			memberCache.Unlock()
			<-pending
			continue
		}
		pending := make(chan struct{})
		memberLoads[key] = pending
		memberCache.Unlock()
		defer func() {
			memberCache.Lock()
			delete(memberLoads, key)
			close(pending)
			memberCache.Unlock()
		}()
		break
	}
	// Duplicate readers share one load; unrelated members can inflate independently.
	// Bound disk/CPU pressure without holding the cache map lock across I/O.
	memberSlots <- struct{}{}
	defer func() { <-memberSlots }()
	tmp, err := os.CreateTemp("", "rommeta-member-*.tmp")
	if err != nil {
		r.Close()
		archive.Close()
		return nil, err
	}
	n, e := io.Copy(tmp, io.LimitReader(r, MaxMemberSize+1))
	ce := r.Close()
	archive.Close()
	if e == nil {
		e = ce
	}
	if e == nil && n != info.Size() {
		e = fmt.Errorf("ZIP member size changed")
	}
	if e != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, e
	}
	if _, e = tmp.Seek(0, io.SeekStart); e != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, e
	}
	memberCache.Lock()
	defer memberCache.Unlock()
	old := memberCache.entries[key]
	if old != nil && old.refs == 0 {
		os.Remove(old.name)
	}
	entry := &cachedEntry{name: tmp.Name(), info: info, refs: 1, used: time.Now()}
	memberCache.entries[key] = entry
	evictMembers(false)
	return &File{File: tmp, info: info, release: releaseMember(entry)}, nil
}

func releaseMember(entry *cachedEntry) func() {
	return func() {
		memberCache.Lock()
		defer memberCache.Unlock()
		entry.refs--
		entry.used = time.Now()
		found := false
		for _, e := range memberCache.entries {
			if e == entry {
				found = true
				break
			}
		}
		if !found && entry.refs == 0 {
			os.Remove(entry.name)
		}
		evictMembers(false)
	}
}
func evictMembers(all bool) {
	for {
		var bytes int64
		var oldest *cachedEntry
		var key string
		for k, e := range memberCache.entries {
			bytes += e.info.Size()
			if e.refs == 0 && (oldest == nil || e.used.Before(oldest.used)) {
				oldest = e
				key = k
			}
		}
		if oldest == nil || !all && bytes <= memberCacheBytes && len(memberCache.entries) <= memberCacheEntries {
			return
		}
		if e := os.Remove(oldest.name); e != nil && !os.IsNotExist(e) {
			return
		}
		delete(memberCache.entries, key)
	}
}

// Cleanup removes idle disposable member files at normal CLI shutdown. An
// abruptly stopped process can leave files in the OS temporary directory.
func Cleanup() { memberCache.Lock(); defer memberCache.Unlock(); evictMembers(true) }
