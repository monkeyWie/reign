// Package probecmd provides the optional public TCP/UDP discovery service.
// Signaling, device IDs, pairing and relay belong to the Worker.
package probecmd

import (
	"context"
	"flag"
	"github.com/monkeyWie/reign/internal/connectivity"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func Run() {
	listen := flag.String("listen", ":26330", "STUN Binding listener on both TCP and UDP; expose directly, not through HTTP proxy")
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
	log.Printf("Reign probe TCP %s; UDP %s", tcp.Addr(), udp.Addr())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
