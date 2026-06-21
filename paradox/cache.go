package paradox

import (
	"os"
	"sync"
)

// tableCache caches parsed tables keyed by file path. Because the .DB files are
// written by an external application, each lookup re-stats the file and only
// re-parses when its modification time or size changed. Parsing a large table
// costs hundreds of milliseconds, while os.Stat costs microseconds, so this
// turns repeated reads into near-free map lookups under high request rates.
type cacheEntry struct {
	mu      sync.Mutex // serializes parsing for this single path (singleflight)
	table   *Table
	modTime int64
	size    int64
}

var (
	cacheMu sync.RWMutex
	cache   = map[string]*cacheEntry{}
)

// OpenTable returns a parsed table for path, reusing a cached parse when the
// underlying file is unchanged. The returned *Table is shared and must be
// treated as read-only by callers.
func OpenTable(path string) (*Table, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	mod := fi.ModTime().UnixNano()
	size := fi.Size()

	cacheMu.RLock()
	entry := cache[path]
	cacheMu.RUnlock()
	if entry == nil {
		cacheMu.Lock()
		if entry = cache[path]; entry == nil {
			entry = &cacheEntry{}
			cache[path] = entry
		}
		cacheMu.Unlock()
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.table != nil && entry.modTime == mod && entry.size == size {
		return entry.table, nil
	}

	table, err := ReadTable(path)
	if err != nil {
		return nil, err
	}
	entry.table = table
	entry.modTime = mod
	entry.size = size
	return table, nil
}
