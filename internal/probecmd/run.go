// Package probecmd provides the optional public TCP/UDP discovery service.
// The full service is provided by servercmd; this entry point is probe-only.
package probecmd

import (
	"context"
	"flag"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func Run() {
	listen := flag.String("listen", ":"+connectivity.DefaultSTUNPort, "STUN Binding listener on both TCP and UDP; expose directly, not through HTTP proxy")
	flag.Parse()
	tcp, err := connectivity.ListenTCPSTUN(*listen)
	if err != nil {
		log.Fatal(err)
	}
	defer tcp.Close()
	udp, err := connectivity.ListenSTUN(*listen)
	if err != nil {
		tcp.Close()
		log.Fatal(err)
	}
	defer udp.Close()
	log.Printf("ReignHub probe TCP %s; UDP %s", tcp.Addr(), udp.Addr())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
