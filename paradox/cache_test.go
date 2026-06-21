package paradox

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenTableCachesAndReuses(t *testing.T) {
	path, err := FindTableFile("../tables", "Setting")
	if err != nil {
		t.Skipf("Setting table not available: %v", err)
	}

	a, err := OpenTable(path)
	if err != nil {
		t.Fatalf("OpenTable: %v", err)
	}
	b, err := OpenTable(path)
	if err != nil {
		t.Fatalf("OpenTable (2nd): %v", err)
	}
	// Unchanged file must return the exact same cached instance.
	if a != b {
		t.Errorf("expected cached table to be reused, got distinct instances")
	}
}

func TestOpenTableReparsesOnChange(t *testing.T) {
	src, err := FindTableFile("../tables", "Setting")
	if err != nil {
		t.Skipf("Setting table not available: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), "Cache.DB")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	a, err := OpenTable(tmp)
	if err != nil {
		t.Fatalf("OpenTable: %v", err)
	}
	// Bump mtime so the cache detects a change and re-parses.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(tmp, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	b, err := OpenTable(tmp)
	if err != nil {
		t.Fatalf("OpenTable (after change): %v", err)
	}
	if a == b {
		t.Errorf("expected re-parse after mtime change, got same instance")
	}
	if len(a.Records) != len(b.Records) {
		t.Errorf("record count changed across re-parse: %d vs %d", len(a.Records), len(b.Records))
	}
}

func TestOpenTableConcurrent(t *testing.T) {
	path, err := FindTableFile("../tables", "Setting")
	if err != nil {
		t.Skipf("Setting table not available: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := OpenTable(path); err != nil {
				t.Errorf("concurrent OpenTable: %v", err)
			}
		}()
	}
	wg.Wait()
}
