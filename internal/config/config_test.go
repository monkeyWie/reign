package config

import (
	"github.com/monkeyWie/reignhub/pkg/appdb"
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

func TestSQLiteSettingsSurviveReload(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.ServiceURL = "https://instance.example"
	c.DeviceName = "ReignHub desktop"
	if err = c.Save(); err != nil {
		t.Fatal(err)
	}
	// SQLite is authoritative; a stale JSON file must not reset saved settings.
	if err = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"serviceUrl":"https://old.example"}`), 0600); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.ServiceURL != c.ServiceURL || again.DeviceName != c.DeviceName {
		t.Fatal("lost saved settings")
	}
	raw, err := os.ReadFile(appdb.Path(dir))
	if err != nil || len(raw) < 16 || string(raw[:16]) != "SQLite format 3\x00" {
		t.Fatal("settings are not SQLite", err)
	}
}
