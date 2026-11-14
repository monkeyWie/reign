// agenthub-demo runs a local service and fake controlled device. No real Agents are touched.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/monkeyWie/reignhub/internal/app"
	"github.com/monkeyWie/reignhub/internal/assets"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"github.com/monkeyWie/reignhub/internal/host"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/store"
	"github.com/monkeyWie/reignhub/internal/syncstate"
	"github.com/monkeyWie/reignhub/internal/testagent"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7443", "local HTTP listen address")
	web := flag.String("web", "", "optional Flutter Web build directory")
	password := flag.String("password", "demo-password", "fake host pairing password (test data only)")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dir, e := os.MkdirTemp("", "agenthub-demo-*")
	if e != nil {
		log.Fatal(e)
	}
	defer os.RemoveAll(dir)
	svc, e := deviceservice.New(filepath.Join(dir, "registry.json"), []string{"http://localhost:*", "http://127.0.0.1:*"})
	if e != nil {
		log.Fatal(e)
	}
	ln, e := net.Listen("tcp", *listen)
	if e != nil {
		log.Fatal(e)
	}
	// Demo mode is intentionally restricted to loopback.
	ip := ln.Addr().(*net.TCPAddr).IP
	if !ip.IsLoopback() {
		log.Fatal("demo binds loopback only")
	}
	mux := http.NewServeMux()
	mux.Handle("/v2/", svc)
	mux.Handle("/relay", svc)
	if *web != "" {
		mux.Handle("/", http.FileServer(http.Dir(*web)))
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(ln)
	defer server.Close()
	cfg, e := config.Load(filepath.Join(dir, "host"))
	if e != nil {
		log.Fatal(e)
	}
	cfg.ServiceURL = "http://" + ln.Addr().String()
	cfg.DeviceName = "Demo · 测试电脑"
	db, e := store.Open(filepath.Join(dir, "host.db"))
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	archive := assets.New(filepath.Join(dir, "attachments"))
	d := &control.Dispatcher{Assets: archive, Providers: map[string]provider.Provider{"demo": testagent.New(archive)}, Execute: db.Execute}
	index := &syncstate.Index{DB: db, Epoch: device.Random()}
	d.Sync = func(ctx context.Context, p provider.Provider, r control.Request) (any, error) {
		return index.Sync(ctx, p, r.Provider, r.Epoch, r.After)
	}
	m := host.New(cfg, d, app.ICE)
	d.Local = m.Local
	go m.Run(ctx)
	for ctx.Err() == nil {
		raw, _ := m.Local(ctx, control.Request{Method: "host.info"})
		info := raw.(map[string]any)
		if info["connectionId"] != "" && m.Network().State == "connected" {
			params, _ := json.Marshal(map[string]string{"password": *password})
			if _, e = m.Local(ctx, control.Request{Method: "host.password", Params: params}); e != nil {
				log.Fatal(e)
			}
			b, _ := json.Marshal(map[string]any{"service": cfg.ServiceURL, "code": info["connectionId"], "public": info["public"], "password": *password})
			fmt.Println(string(b))
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	<-ctx.Done()
}
