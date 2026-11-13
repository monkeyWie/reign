package main

import (
	"context"
	"flag"
	"github.com/monkeyWie/reign/internal/relay"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7443", "listen address (use a TLS reverse proxy for public access)")
	cert := flag.String("tls-cert", "", "TLS certificate")
	key := flag.String("tls-key", "", "TLS private key")
	flag.Parse()
	token := os.Getenv("AGENTHUB_RELAY_TOKEN")
	if len(token) < 32 {
		log.Fatal("set AGENTHUB_RELAY_TOKEN to a random secret of at least 32 characters")
	}
	backend := relay.New(token)
	for _, origin := range strings.Split(os.Getenv("AGENTHUB_ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			backend.Origins = append(backend.Origins, origin)
		}
	}
	s := &http.Server{Addr: *listen, Handler: backend, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); _ = s.Close() }()
	log.Printf("relay listening at %s", *listen)
	var err error
	if *cert != "" {
		err = s.ListenAndServeTLS(*cert, *key)
	} else {
		err = s.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
