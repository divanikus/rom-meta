package catalogfs

import (
	"container/list"
	"strings"
	"sync"
)

// Keep hot ZIP inventories when large platforms exceed the bound; do not flush
// every game's index at once. A game's full and metadata-only views share invalidation.
type zipIndexes struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   list.List
}
type zipIndexEntry struct {
	key   string
	index *gameIndex
}

var indexes = zipIndexes{entries: map[string]*list.Element{}}

func (c *zipIndexes) Load(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[key]
	if e == nil {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(zipIndexEntry).index, true
}
func (c *zipIndexes) Store(key string, index *gameIndex) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil {
		e.Value = zipIndexEntry{key, index}
		c.order.MoveToFront(e)
	} else {
		c.entries[key] = c.order.PushFront(zipIndexEntry{key, index})
	}
	for len(c.entries) > 4096 {
		e := c.order.Back()
		delete(c.entries, e.Value.(zipIndexEntry).key)
		c.order.Remove(e)
	}
}
func (c *zipIndexes) Delete(game string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range []string{game, game + "\x00metadata.zip"} {
		if e := c.entries[key]; e != nil {
			delete(c.entries, key)
			c.order.Remove(e)
		}
	}
}

// A non-media logical path can only belong to metadata.zip. Directory listings
// and writes still inspect the complete game and validate cross-pack collisions.
func memberInventory(l location) (*gameIndex, error) {
	if l.member != "media" && !strings.HasPrefix(l.member, "media/") && !isPack(l.member) {
		return inventory(l.game, "metadata.zip")
	}
	return inventory(l.game)
}
