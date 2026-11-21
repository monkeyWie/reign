package appdb

import (
	"context"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/pkg/persistence"
	"github.com/monkeyWie/reignhub/pkg/persistence/testsupport"
)

func TestHistory(t *testing.T) {
	db, err := Open(Path(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	testsupport.CheckHistory(t, db)
	testsupport.CheckCompetingWriters(t, db, db)
}

func TestLocalHistoryWhileDraftWriterHoldsLock(t *testing.T) {
	path := Path(t.TempDir())
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key := persistence.Session{Env: "local", Provider: "fixture", ID: "chat"}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO drafts(env,provider,session,text) VALUES('local','fixture','chat','draft')"); err != nil {
		t.Fatal(err)
	}
	patch := persistence.Diff("", nil, persistence.Object{
		"turns":         []persistence.Object{{"id": "t", "items": []persistence.Object{{"id": "sent", "role": "user", "text": "fixture"}}}},
		"localMessages": []persistence.Object{{"id": "new", "status": "accepted"}},
	})
	done := make(chan error, 1)
	go func() { done <- db.ApplyHistory(ctx, key, patch) }()
	select {
	case err := <-done:
		t.Fatalf("history returned while draft still held the write lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	page, err := reopened.ReadPage(ctx, key, persistence.PageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	items := persistence.Objects(persistence.Objects(page.Transcript["turns"])[0]["items"])
	pending := persistence.Objects(page.Transcript["localMessages"])
	if len(items) != 1 || items[0]["id"] != "sent" || len(pending) != 1 || pending[0]["status"] != "accepted" {
		t.Fatal("lost local history or delivery status after reopening")
	}
}
