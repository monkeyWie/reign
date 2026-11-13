package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAccessKeyIsAtomicPrivateAndStable(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	keys := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := AccessKey(dir)
			if err != nil {
				t.Error(err)
			}
			keys <- key
		}()
	}
	wg.Wait()
	close(keys)
	var first string
	for key := range keys {
		if first == "" {
			first = key
		}
		if key != first || len(key) != 64 {
			t.Fatal("inconsistent key")
		}
	}
	st, err := os.Stat(filepath.Join(dir, "access-key"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0077 != 0 {
		t.Fatal("key permissions too broad")
	}
	if err = os.WriteFile(filepath.Join(dir, "access-key"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = AccessKey(dir); err == nil {
		t.Fatal("empty authentication key accepted")
	}
}
