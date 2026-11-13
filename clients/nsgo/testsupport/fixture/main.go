//go:build !js

// Isolated local transport fixture. Never opens a real agent or user session.
//
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/relay"
	"github.com/monkeyWie/reign/internal/remote"
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
func main() {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		panic(e)
	}
	url := "http://" + ln.Addr().String()
	p := remote.Pairing{Relay: "ws" + strings.TrimPrefix(url, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: strings.Repeat("r", 32)}
	r := relay.New(p.RelayToken)
	r.Origins = []string{"127.0.0.1:*", "localhost:*"}
	mux := http.NewServeMux()
	mux.Handle("/relay", r)
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]any{"Relay": p.Relay, "Host": p.Host, "Key": p.Key, "Token": p.RelayToken})
	})
	go http.Serve(ln, mux)
	monitor := remote.NewHostStatus(p.Relay)
	go remote.RunHostObserved(context.Background(), p, remote.Options{}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": fake{}}}, monitor)
	for monitor.Snapshot().State != "connected" {
		time.Sleep(time.Millisecond)
	}
	if len(os.Args) > 1 {
		os.WriteFile(os.Args[1], []byte(url), 0600)
	}
	fmt.Println(url)
	select {}
}
