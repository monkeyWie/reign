//go:build !js

package hubui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/pkg/appdb"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestWindowedNativeHistoryThroughController(t *testing.T) {
	dir := t.TempDir()
	db, e := appdb.Open(appdb.Path(dir))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	api := &nativeAPI{dir: dir}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, api)
		s.selected = Object{"providerId": "fixture", "id": "session"}
		s.target = "local"
		rows := make([]Object, 500)
		for i := range rows {
			rows[i] = Object{"id": fmt.Sprint(i), "role": "assistant", "text": strings.Repeat("a", 1024)}
		}
		s.setTranscript(Object{"turns": []Object{{"id": "t", "items": rows}}})
		s.persistSnapshot()
		return core.View{}
	})
	poll := func(match func() bool) {
		t.Helper()
		app.WaitUntil(5*time.Second, match)
	}
	poll(func() bool {
		return flag(s.transcript, "_localOlder") && len(objects(objects(s.transcript["turns"])[0]["items"])) == 150
	})
	if _, err := db.Exec("CREATE TABLE edits(item TEXT); CREATE TRIGGER track_item AFTER UPDATE ON messages BEGIN INSERT INTO edits VALUES(NEW.item); END"); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		s.transcript, _ = applyEvents(s.transcript, []Object{{"kind": "text", "turnId": "t", "itemId": "499", "offset": 1024, "delta": "!"}})
		s.persistSnapshot()
	})
	app.WaitUntil(5*time.Second, func() bool { var n int; return db.QueryRow("SELECT count(*) FROM edits").Scan(&n) == nil && n > 0 })
	var edits, count int
	if e = db.QueryRow("SELECT (SELECT count(*) FROM edits),(SELECT count(*) FROM messages)").Scan(&edits, &count); e != nil || edits != 1 || count != 500 {
		t.Fatal(edits, count, e)
	}
	app.Post(func() { s.older() })
	poll(func() bool { return s.reading != nil && len(objects(objects(s.reading["turns"])[0]["items"])) == 100 })
	page, err := api.ReadPage(context.Background(), persistence.Session{Env: "local", Provider: "fixture", ID: "session"}, persistence.PageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	items := objects(objects(page.Transcript["turns"])[0]["items"])
	if len(items) != 50 || str(items[0], "id") != "450" || !strings.HasSuffix(str(items[49], "text"), "!") {
		t.Fatal("incorrect offline tail")
	}
}

func timelineTestDAO(t *testing.T) persistence.HistoryDAO {
	t.Helper()
	db, err := appdb.Open(appdb.Path(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
