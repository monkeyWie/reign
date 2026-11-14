package remote

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
)

type testProvider struct {
	provider.Provider
	sends   atomic.Int32
	updates atomic.Int32
	log     *provider.EventLog
}

func (p *testProvider) Watch(ctx context.Context, session, epoch string, after int64) (provider.StreamBatch, error) {
	return p.log.Watch(ctx, session, epoch, after)
}

func (p *testProvider) Status(context.Context) provider.Status {
	return provider.Status{ID: "test", State: "ready"}
}
func (p *testProvider) Sessions(context.Context, string) (provider.SessionPage, error) {
	return provider.SessionPage{Sessions: []provider.Session{{ID: "original-thread", Title: strconv.Itoa(int(p.updates.Load())), Preview: strings.Repeat("消息", 300000)}}}, nil
}
func (p *testProvider) Send(ctx context.Context, id, text string) error {
	if id != "original-thread" || text != "continue" {
		panic("session identity changed")
	}
	p.sends.Add(1)
	return nil
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func TestRelayAndP2P(t *testing.T) {
	for _, tc := range []struct {
		name       string
		options    Options
		path       string
		directOnly bool
	}{
		{"Relay", Options{RelayOnly: true}, "Relay", false},
		{"UDP", Options{DisableTCP: true}, "P2P · WebRTC", false},
		{"TCP", Options{DisableWebRTC: true}, "P2P · TCP", false},
		{"UDP free", Options{DisableTCP: true}, "P2P · WebRTC", true},
		{"TCP free", Options{DisableWebRTC: true}, "P2P · TCP", true},
	} {
		name, opts := tc.name, tc.options
		relayOnly := opts.RelayOnly
		t.Run(name, func(t *testing.T) {
			token := strings.Repeat("r", 32)
			relayURL := os.Getenv("AGENTHUB_TEST_RELAY_URL")
			if relayURL == "" {
				fixture := relay.New(token)
				if tc.directOnly {
					fixture.RelayAllowed = func(string, string, string) bool { return false }
				}
				server := httptest.NewServer(fixture)
				defer server.Close()
				relayURL = "ws" + strings.TrimPrefix(server.URL, "http")
			} else {
				token = os.Getenv("AGENTHUB_TEST_RELAY_TOKEN")
				if len(token) < 32 {
					t.Fatal("external relay test requires AGENTHUB_TEST_RELAY_TOKEN")
				}
			}
			p := Pairing{Relay: relayURL, Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
			fake := &testProvider{log: provider.NewEventLog()}
			dispatcher := &control.Dispatcher{Providers: map[string]provider.Provider{"test": fake}}
			monitor := NewHostStatus(p.Relay)
			dispatcher.Network = monitor.Snapshot
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hostDone := make(chan error, 1)
			hostOpts := opts
			go func() { hostDone <- serveHostObserved(ctx, p, hostOpts, dispatcher, monitor) }()
			waitFor(t, func() bool { return monitor.Snapshot().State == "connected" })
			var traceMu sync.Mutex
			trace := []connectivity.Progress{}
			opts.Progress = func(p connectivity.Progress) { traceMu.Lock(); trace = append(trace, p); traceMu.Unlock() }
			var client *Client
			waitFor(t, func() bool {
				attempt, stop := context.WithTimeout(ctx, 4*time.Second)
				defer stop()
				var err error
				client, err = Connect(attempt, p, opts)
				return err == nil
			})
			defer func() {
				client.Close()
				cancel()
				select {
				case <-hostDone:
				case <-time.After(3 * time.Second):
					t.Error("host leaked")
				}
			}()
			var network control.NetworkStatus
			if err := client.Call(ctx, control.Request{Method: "network"}, &network); err != nil {
				t.Fatal(err)
			}
			if network.State != "connected" || len(network.Peers) != 1 || network.Peers[0].ID != client.wire.peer {
				t.Fatalf("authenticated client missing from host status: %+v", network)
			}
			if !relayOnly {
				waitFor(t, func() bool { return client.Path() == tc.path })
			}
			waitFor(t, func() bool {
				traceMu.Lock()
				defer traceMu.Unlock()
				for _, step := range trace {
					if strings.HasPrefix(step.ID, "path:") && step.State == "success" {
						return true
					}
				}
				return false
			})
			if strings.HasPrefix(name, "TCP") {
				if err := client.wire.tcpSnapshot().control(tcpPing); err != nil {
					t.Fatal("encrypted TCP heartbeat failed", err)
				}
				var state control.NetworkStatus
				if err := client.Call(ctx, control.Request{Method: "network"}, &state); err != nil || client.Path() != tc.path || len(state.Peers) != 1 || state.Peers[0].Path != tc.path {
					t.Fatal("TCP heartbeat broke the selected channel", state, err)
				}
			}
			var baseline, updates provider.StreamBatch
			if err := client.Call(ctx, control.Request{Method: "watch", Provider: "test", Session: "original-thread"}, &baseline); err != nil || !baseline.Reset {
				t.Fatal("live subscription failed", err)
			}
			fake.log.Publish(provider.StreamEvent{SessionID: "original-thread", TurnID: "turn", ItemID: "message", Kind: "text", Delta: "partial"})
			if err := client.Call(ctx, control.Request{Method: "watch", Provider: "test", Session: "original-thread", Epoch: baseline.Epoch, After: baseline.Cursor}, &updates); err != nil || len(updates.Events) != 1 || updates.Events[0].Delta != "partial" {
				t.Fatal("partial output did not cross the active transport", err)
			}
			var page provider.SessionPage
			requestCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			defer stop()
			if err := client.wire.ws.Ping(requestCtx); err != nil {
				t.Fatalf("relay control ping failed: %v", err)
			}
			t.Logf("active path: %s", client.Path())
			if err := client.Call(requestCtx, control.Request{Method: "sessions", Provider: "test"}, &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Sessions) != 1 || page.Sessions[0].ID != "original-thread" || len(page.Sessions[0].Preview) != 1800000 {
				t.Fatal("large transcript did not survive chunking")
			}
			// Unchanged history is returned from the validated client snapshot;
			// a later update must invalidate that snapshot on both transports.
			var unchanged provider.SessionPage
			if err := client.Call(requestCtx, control.Request{Method: "sessions", Provider: "test"}, &unchanged); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(page, unchanged) {
				t.Fatal("conditional history differs from original snapshot")
			}
			fake.updates.Add(1)
			var changed provider.SessionPage
			if err := client.Call(requestCtx, control.Request{Method: "sessions", Provider: "test"}, &changed); err != nil {
				t.Fatal(err)
			}
			if changed.Sessions[0].Title != "1" {
				t.Fatal("conditional history did not refresh after mutation")
			}
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := client.Call(requestCtx, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil); err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			if !relayOnly && client.Path() != tc.path {
				t.Fatalf("direct channel unexpectedly fell back during transfer: %s", client.Path())
			}
			if fake.sends.Load() != 4 {
				t.Fatalf("executed %d times", fake.sends.Load())
			}
			if !relayOnly {
				client.wire.mu.Lock()
				pc := client.wire.pc
				client.wire.mu.Unlock()
				if strings.HasPrefix(name, "TCP") {
					client.wire.tcpSnapshot().close()
				} else {
					_ = pc.Close()
					// Explicitly disable both fixture endpoints. Remote ICE loss
					// detection is asynchronous; this test verifies relay routing
					// and exactly-once dispatch after the direct path is unavailable.
					monitor.mu.Lock()
					hostWire := monitor.peers[client.wire.peer]
					monitor.mu.Unlock()
					hostWire.mu.Lock()
					hostPC := hostWire.pc
					hostWire.mu.Unlock()
					_ = hostPC.Close()
				}
				if tc.directOnly {
					waitFor(t, func() bool { return !strings.HasPrefix(client.Path(), "P2P") })
					if client.wire.dataWS != nil {
						t.Fatal("free connection opened relay")
					}
					short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
					defer stop()
					if client.Call(short, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil) == nil {
						t.Fatal("business data bypassed denied relay")
					}
					if fake.sends.Load() != 4 {
						t.Fatal("denied relay executed a command")
					}
					return
				}
				waitFor(t, func() bool {
					peers := monitor.Snapshot().Peers
					return strings.HasPrefix(client.Path(), "Relay") && len(peers) == 1 && strings.HasPrefix(peers[0].Path, "Relay")
				})
				// Fault injection gets its own budget rather than sharing the
				// deadline with the large-message and concurrent-send checks.
				fallbackCtx, cancelFallback := context.WithTimeout(ctx, 15*time.Second)
				defer cancelFallback()
				if err := client.Call(fallbackCtx, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil); err != nil {
					t.Fatalf("fallback failed: %v", err)
				}
				if fake.sends.Load() != 5 {
					t.Fatal("fallback duplicated or lost request")
				}
			}
		})
	}
}
func TestPairingAndAuthenticatedEncryption(t *testing.T) {
	key := strings.Repeat("ab", 32)
	sender, _ := newCryptor(key)
	receiver, _ := newCryptor(key)
	packet, err := sender.seal("peer/c2h/data", []byte("private conversation"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(packet), "private conversation") {
		t.Fatal("plaintext leaked")
	}
	if _, err = receiver.open("different-peer/c2h/data", packet); err == nil {
		t.Fatal("AAD not bound")
	}
	corrupt := append([]byte(nil), packet...)
	corrupt[len(corrupt)-1] ^= 1
	if _, err = receiver.open("peer/c2h/data", corrupt); err == nil {
		t.Fatal("tampering accepted")
	}
	plain, err := receiver.open("peer/c2h/data", packet)
	if err != nil || string(plain) != "private conversation" {
		t.Fatal(err)
	}
	if _, err = receiver.open("peer/c2h/data", packet); err == nil {
		t.Fatal("replay accepted")
	}
	other, _ := newCryptor(strings.Repeat("cd", 32))
	if _, err = other.open("peer/c2h/data", packet); err == nil {
		t.Fatal("wrong key accepted")
	}
	p := Pairing{Relay: "wss://example.com", Host: relay.ID(), Key: key, RelayToken: "token"}
	parsed, err := Parse(p.Code())
	if err != nil || !reflect.DeepEqual(parsed, p) {
		t.Fatal("pairing roundtrip failed", err)
	}
	raw, _ := json.Marshal(p)
	if _, err = Parse(string(raw)); err == nil {
		t.Fatal("unversioned pairing accepted")
	}
}
func TestRelayRejectsCredentials(t *testing.T) {
	server := httptest.NewServer(relay.New(strings.Repeat("r", 32)))
	defer server.Close()
	p := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: "wrong"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, _, err := dial(ctx, p, "host"); err == nil {
		t.Fatal("relay accepted invalid credentials")
	}
}

func TestFreshHandshakeSeparatesReusedRelayIDs(t *testing.T) {
	key := strings.Repeat("ab", 32)
	first := helloMessage{ClientNonce: relay.ID() + relay.ID(), HostNonce: relay.ID() + relay.ID()}
	second := helloMessage{ClientNonce: first.ClientNonce, HostNonce: relay.ID() + relay.ID()}
	sender, _ := sessionCryptor(key, "reused-relay-id", first, false)
	receiver, _ := sessionCryptor(key, "reused-relay-id", first, true)
	newConnection, _ := sessionCryptor(key, "reused-relay-id", second, true)
	packet, err := sender.seal("peer/c2h/chunk", []byte("send instruction"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = receiver.open("peer/c2h/chunk", packet); err != nil {
		t.Fatal(err)
	}
	if _, err = newConnection.open("peer/c2h/chunk", packet); err == nil {
		t.Fatal("old command replayed into a new session")
	}
	base1, _ := newCryptor(key)
	base1.random = true
	base2, _ := newCryptor(key)
	base2.random = true
	one, _ := base1.seal("hello", []byte("challenge"))
	two, _ := base2.seal("hello", []byte("challenge"))
	if string(one[:12]) == string(two[:12]) {
		t.Fatal("long-term handshake nonces reused")
	}
}
