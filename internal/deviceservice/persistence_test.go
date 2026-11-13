package deviceservice

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"github.com/monkeyWie/reign/internal/device"
	"gorm.io/gorm"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteReopenAndPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reign.db")
	s, err := New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(s)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	create := func(host bool) device.Identity {
		t.Helper()
		i, err := device.NewIdentity("original")
		if err != nil {
			t.Fatal(err)
		}
		i, err = device.Register(ctx, httpServer.URL, i, host)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	h, c := create(true), create(false)
	if err = s.DB.Create(&DeviceGrant{Host: h.ID, Client: c.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err = device.Post(ctx, httpServer.URL, h.Token, "/p2p/devices/presence", map[string]string{"name": "renamed"}, nil); err != nil {
		t.Fatal(err)
	}
	sid := s.ServiceID
	httpServer.Close()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.ServiceID != sid || !s.CanAccess(c.ID, h.ID) {
		t.Fatal("lost service identity or pairing on reopen")
	}
	got, ok := s.Authenticate(h.Token)
	if !ok || got.Name != "renamed" || got.TokenHash != "" {
		t.Fatalf("incorrect restored identity: %+v", got)
	}
	for _, suffix := range []string{"", "-wal"} {
		b, _ := os.ReadFile(path + suffix)
		if strings.Contains(string(b), h.Token) || strings.Contains(string(b), c.Token) {
			t.Fatal("raw credential persisted")
		}
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0600 {
		t.Fatal("insecure database permissions")
	}
	s.Policy = func(context.Context, Admission) error { return errors.New("denied") }
	httpServer = httptest.NewServer(s)
	defer httpServer.Close()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/p2p/signal?id="+h.ID+"&role=host", &websocket.DialOptions{HTTPHeader: map[string][]string{"Authorization": {"Bearer " + h.Token}}})
	if err == nil {
		ws.CloseNow()
		t.Fatal("relay bypassed admission policy")
	}
}
func TestRegistrationHookRollsBack(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.OnRegister = func(tx *gorm.DB, d Device) error {
		if err := tx.Create(&Metadata{Key: "must-rollback", Value: d.ID}).Error; err != nil {
			return err
		}
		return errors.New("quota")
	}
	server := httptest.NewServer(s)
	defer server.Close()
	i, _ := device.NewIdentity("host")
	if _, err = device.Register(context.Background(), server.URL, i, true); err == nil {
		t.Fatal("rejected registration succeeded")
	}
	var count int64
	s.DB.Model(&Device{}).Count(&count)
	if count != 0 {
		t.Fatal("orphan device")
	}
	s.DB.Model(&Metadata{}).Where("key = ?", "must-rollback").Count(&count)
	if count != 0 {
		t.Fatal("hook transaction not rolled back")
	}
}
