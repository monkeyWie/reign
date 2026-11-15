//go:build !js

// Isolated local transport fixture. Never opens a real agent or user session.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/monkeyWie/reignhub/internal/remote"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type fake struct{ provider.Provider }

func (fake) Status(context.Context) provider.Status {
	return provider.Status{ID: "fixture", State: "ready"}
}
func (fake) Sessions(context.Context, string) (provider.SessionPage, error) {
	return provider.SessionPage{Sessions: []provider.Session{{ID: "isolated", Preview: strings.Repeat("fixture🙂", 20000)}}}, nil
}

type stalledAgent struct{ fake }

func (stalledAgent) Status(ctx context.Context) provider.Status {
	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
	}
	return provider.Status{ID: "fixture", State: "ready"}
}
func main() {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		panic(e)
	}
	url := "http://" + ln.Addr().String()
	p := remote.Pairing{Relay: "ws" + strings.TrimPrefix(url, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: strings.Repeat("r", 32)}
	direct := p
	direct.Host = relay.ID()
	slow := p
	slow.Host = relay.ID()
	r := relay.New(p.RelayToken)
	r.RelayAllowed = func(host, _, _ string) bool { return host != direct.Host }
	r.Origins = []string{"127.0.0.1:*", "localhost:*"}
	mux := http.NewServeMux()
	mux.Handle("/p2p/signal", r)
	mux.HandleFunc("/p2p/relay", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("id") == slow.Host {
			select {
			case <-time.After(6 * time.Second):
			case <-req.Context().Done():
				return
			}
		}
		r.ServeHTTP(w, req)
	})
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]any{"Relay": p.Relay, "Host": p.Host, "Key": p.Key, "Token": p.RelayToken})
	})
	mux.HandleFunc("/fixture-direct", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]any{"Relay": direct.Relay, "Host": direct.Host, "Key": direct.Key, "Token": direct.RelayToken})
	})
	mux.HandleFunc("/fixture-slow", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]any{"Relay": slow.Relay, "Host": slow.Host, "Key": slow.Key, "Token": slow.RelayToken})
	})
	go http.Serve(ln, mux)
	monitor := remote.NewHostStatus(p.Relay)
	go remote.RunHostObserved(context.Background(), p, remote.Options{}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": fake{}}}, monitor)
	directMonitor := remote.NewHostStatus(direct.Relay)
	go remote.RunHostObserved(context.Background(), direct, remote.Options{}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": fake{}}}, directMonitor)
	slowMonitor := remote.NewHostStatus(slow.Relay)
	go remote.RunHostObserved(context.Background(), slow, remote.Options{}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": stalledAgent{}}}, slowMonitor)
	for monitor.Snapshot().State != "connected" || directMonitor.Snapshot().State != "connected" || slowMonitor.Snapshot().State != "connected" {
		time.Sleep(time.Millisecond)
	}
	if len(os.Args) > 1 {
		os.WriteFile(os.Args[1], []byte(url), 0600)
	}
	fmt.Println(url)
	select {}
}
