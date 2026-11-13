package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Cache is a bounded, disposable controller cache, separate from the host's
// attachment archive. Device namespaces include the service and host identity.
type Cache struct {
	Root  string
	Limit int64
	mu    sync.Mutex
}
type cacheEntry struct {
	Device  string `json:"device"`
	Key     string `json:"key"`
	Size    int64  `json:"size"`
	Touched int64  `json:"touched"`
}

func (c *Cache) name(device, key string) string { return digest(device + "\x00" + key) }
func (c *Cache) Get(device, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name := c.name(device, key)
	metaPath := filepath.Join(c.Root, name+".json")
	b, e := os.ReadFile(metaPath)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var entry cacheEntry
	if json.Unmarshal(b, &entry) != nil || entry.Device != device || entry.Key != key {
		return nil, nil
	}
	b, e = os.ReadFile(filepath.Join(c.Root, name+".bin"))
	if e != nil {
		return nil, nil
	}
	entry.Touched = time.Now().UnixMilli()
	_ = writeJSON(metaPath, entry)
	return b, nil
}
func (c *Cache) Put(device, key string, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	limit := c.Limit
	if limit <= 0 {
		limit = 128 << 20
	}
	if int64(len(b)) > MaxFileSize || int64(len(b)) > limit {
		return fmt.Errorf("附件超过缓存容量")
	}
	if e := os.MkdirAll(c.Root, 0700); e != nil {
		return e
	}
	entries, _ := os.ReadDir(c.Root)
	all := []struct {
		name  string
		entry cacheEntry
	}{}
	var total int64
	name := c.name(device, key)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(c.Root, entry.Name()))
		var value cacheEntry
		if e != nil || json.Unmarshal(raw, &value) != nil {
			continue
		}
		if entry.Name() == name+".json" {
			continue
		}
		total += value.Size
		all = append(all, struct {
			name  string
			entry cacheEntry
		}{entry.Name(), value})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].entry.Touched < all[j].entry.Touched })
	for _, entry := range all {
		if total+int64(len(b)) <= limit {
			break
		}
		_ = os.Remove(filepath.Join(c.Root, entry.name))
		_ = os.Remove(filepath.Join(c.Root, entry.name[:len(entry.name)-5]+".bin"))
		total -= entry.entry.Size
	}
	f, e := os.CreateTemp(c.Root, ".cache-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), filepath.Join(c.Root, name+".bin")); e != nil {
		return e
	}
	return writeJSON(filepath.Join(c.Root, name+".json"), cacheEntry{Device: device, Key: key, Size: int64(len(b)), Touched: time.Now().UnixMilli()})
}
func (c *Cache) Clear(device string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, _ := os.ReadDir(c.Root)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, e := os.ReadFile(filepath.Join(c.Root, entry.Name()))
		var v cacheEntry
		if e != nil || json.Unmarshal(b, &v) != nil {
			continue
		}
		if device != "" && v.Device != device {
			continue
		}
		_ = os.Remove(filepath.Join(c.Root, entry.Name()))
		_ = os.Remove(filepath.Join(c.Root, entry.Name()[:len(entry.Name())-5]+".bin"))
	}
	return nil
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
