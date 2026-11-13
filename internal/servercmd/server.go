package servercmd

import (
	"context"
	"flag"
	"github.com/monkeyWie/reign/internal/connectivity"
	"github.com/monkeyWie/reign/internal/deviceservice"
	"github.com/monkeyWie/reign/internal/webui"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func Run() {
	listen := flag.String("listen", ":26331", "HTTP/WebSocket listen address; TLS is optional")
	stunListen := flag.String("stun-listen", ":"+connectivity.DefaultSTUNPort, "UDP STUN listen address; empty disables STUN")
	stunPublic := flag.String("stun-public", "", "advertised self-hosted STUN URL; default uses request hostname and STUN listen port")
	tcpSTUNListen := flag.String("tcp-stun-listen", ":26330", "TCP STUN listen address; empty disables TCP probing")
	tcpSTUNPublic := flag.String("tcp-stun-public", "", "comma-separated TCP STUN host:port endpoints; default uses request hostname and TCP STUN port")
	publicSTUN := flag.String("public-stun", strings.Join(connectivity.DefaultPolicy().PublicSTUN, ","), "comma-separated public STUN URLs; empty uses only self-hosted STUN")
	state := flag.String("state", "./data/reign.db", "SQLite database path")
	origins := flag.String("origins", "http://localhost:7357,http://127.0.0.1:7357", "exact permitted Web origins")
	web := flag.String("web", "", "optional external Web directory overriding embedded Reign Web")
	noWeb := flag.Bool("no-web", false, "disable embedded and external Web hosting")
	tlsCert := flag.String("tls-cert", "", "optional HTTPS certificate PEM (otherwise use a TLS reverse proxy)")
	tlsKey := flag.String("tls-key", "", "HTTPS private key PEM")
	flag.Parse()
	if (*tlsCert == "") != (*tlsKey == "") {
		log.Fatal("tls-cert and tls-key must be supplied together")
	}
	s, e := deviceservice.New(*state, strings.Split(*origins, ","))
	if e != nil {
		log.Fatal(e)
	}
	defer s.Close()
	s.Network.PublicSTUN = split(*publicSTUN)
	if *stunPublic != "" {
		s.Network.SelfSTUN = split(*stunPublic)
	}
	if *tcpSTUNPublic != "" {
		s.Network.TCPSTUN = split(*tcpSTUNPublic)
	}
	if *tcpSTUNListen != "" {
		tcpSTUN, err := connectivity.ListenTCPSTUN(*tcpSTUNListen)
		if err != nil {
			log.Fatalf("TCP STUN listen: %v", err)
		}
		defer tcpSTUN.Close()
		_, s.TCPSTUNPort, _ = net.SplitHostPort(tcpSTUN.Addr().String())
		log.Printf("Reign STUN TCP %s", tcpSTUN.Addr())
	}
	if *stunListen != "" {
		stun, err := connectivity.ListenSTUN(*stunListen)
		if err != nil {
			log.Fatalf("STUN listen: %v", err)
		}
		defer stun.Close()
		_, s.STUNPort, _ = net.SplitHostPort(stun.Addr().String())
		log.Printf("Reign STUN UDP %s", stun.Addr())
	}
	mux := http.NewServeMux()
	mux.Handle("/v2/", s)
	mux.Handle("/v1/", s)
	mux.Handle("/cloud/", http.NotFoundHandler())
	mux.Handle("/relay", s)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	if !*noWeb {
		if *web != "" {
			mux.Handle("/", http.FileServer(http.Dir(*web)))
		} else {
			mux.Handle("/", webui.Handler())
		}
	}
	log.Printf("Reign device service http://%s", *listen)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	failed := make(chan error, 1)
	go func() {
		if *tlsCert != "" {
			failed <- server.ListenAndServeTLS(*tlsCert, *tlsKey)
		} else {
			failed <- server.ListenAndServe()
		}
	}()
	select {
	case err := <-failed:
		log.Printf("HTTP service stopped: %v", err)
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
}

func split(raw string) []string {
	result := []string{}
	for _, entry := range strings.Split(raw, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			result = append(result, entry)
		}
	}
	return result
}
