// Package servercmd runs the standalone, account-free ReignHub service.
package servercmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"github.com/monkeyWie/reignhub/internal/webui"
)

// Version is set by release builds.
var Version = "dev"

type Config struct {
	HTTP          bool   `json:"http"`
	Signal        bool   `json:"signal"`
	Relay         bool   `json:"relay"`
	WebEnabled    bool   `json:"webEnabled"`
	STUN          bool   `json:"stun"`
	TCPSTUN       bool   `json:"tcpStun"`
	Listen        string `json:"listen"`
	State         string `json:"state"`
	Web           string `json:"web"`
	STUNListen    string `json:"stunListen"`
	TCPSTUNListen string `json:"tcpStunListen"`
	PublicSTUN    string `json:"publicStun"`
	SelfSTUN      string `json:"selfStun"`
	TCPSTUNPublic string `json:"tcpStunPublic"`
	Origins       string `json:"origins"`
	TLSCert       string `json:"tlsCert"`
	TLSKey        string `json:"tlsKey"`
}

func DefaultConfig() Config {
	return Config{HTTP: true, Signal: true, Relay: true, WebEnabled: true, STUN: true, TCPSTUN: true,
		Listen: ":26330", State: "data/reignhub.db", STUNListen: ":" + connectivity.DefaultSTUNPort, TCPSTUNListen: ":" + connectivity.DefaultSTUNPort,
		PublicSTUN: strings.Join(connectivity.DefaultPolicy().PublicSTUN, ",")}
}

// ParseConfig applies defaults, then a strict JSON file, then CLI overrides.
func ParseConfig(args []string, output io.Writer) (Config, bool, error) {
	c := DefaultConfig()
	var file string
	for i, a := range args {
		if strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "-config=") {
			file = strings.SplitN(a, "=", 2)[1]
		}
		if a == "--config" || a == "-config" {
			if i+1 >= len(args) {
				return c, false, fmt.Errorf("--config requires a file")
			}
			file = args[i+1]
		}
	}
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return c, false, err
		}
		defer f.Close()
		d := json.NewDecoder(io.LimitReader(f, 1<<20))
		d.DisallowUnknownFields()
		if err = d.Decode(&c); err != nil {
			return c, false, fmt.Errorf("config: %w", err)
		}
		if err = d.Decode(new(any)); err != io.EOF {
			return c, false, fmt.Errorf("config must contain one JSON object")
		}
	}
	f := flag.NewFlagSet("reignhub-server", flag.ContinueOnError)
	f.SetOutput(output)
	f.StringVar(&file, "config", file, "JSON config file; command-line flags override its values")
	f.BoolVar(&c.HTTP, "http", c.HTTP, "enable HTTP device directory, pairing and selected WebSocket/Web features; false runs probes only")
	f.BoolVar(&c.Signal, "signal", c.Signal, "enable signaling (requires HTTP)")
	f.BoolVar(&c.Relay, "relay", c.Relay, "enable encrypted relay (requires signaling)")
	f.BoolVar(&c.WebEnabled, "web-enabled", c.WebEnabled, "serve the embedded Web console")
	f.BoolVar(&c.STUN, "stun", c.STUN, "enable UDP STUN discovery")
	f.BoolVar(&c.TCPSTUN, "tcp-stun", c.TCPSTUN, "enable TCP STUN discovery")
	f.StringVar(&c.Listen, "listen", c.Listen, "HTTP/HTTPS listen address")
	f.StringVar(&c.State, "state", c.State, "SQLite database path (used only with HTTP)")
	f.StringVar(&c.Web, "web", c.Web, "serve a built console directory instead of embedded assets")
	f.StringVar(&c.STUNListen, "stun-listen", c.STUNListen, "UDP STUN listen address; expose directly")
	f.StringVar(&c.TCPSTUNListen, "tcp-stun-listen", c.TCPSTUNListen, "TCP STUN listen address; expose directly")
	f.StringVar(&c.PublicSTUN, "public-stun", c.PublicSTUN, "comma-separated public UDP STUN URLs; empty disables public discovery")
	f.StringVar(&c.SelfSTUN, "self-stun", c.SelfSTUN, "advertised UDP STUN URLs; empty derives from request host and listener port")
	f.StringVar(&c.TCPSTUNPublic, "tcp-stun-public", c.TCPSTUNPublic, "advertised TCP probe host:port list; empty derives from request host")
	f.StringVar(&c.Origins, "origins", c.Origins, "additional allowed browser origins, comma-separated exact URLs")
	f.StringVar(&c.TLSCert, "tls-cert", c.TLSCert, "TLS certificate PEM; otherwise terminate HTTPS at a reverse proxy")
	f.StringVar(&c.TLSKey, "tls-key", c.TLSKey, "TLS private key PEM")
	version := f.Bool("version", false, "print version and exit")
	if err := f.Parse(args); err != nil {
		return c, false, err
	}
	if f.NArg() != 0 {
		return c, false, fmt.Errorf("unexpected arguments: %s", strings.Join(f.Args(), " "))
	}
	if *version {
		return c, true, nil
	}
	return c, false, c.Validate()
}

func (c Config) Validate() error {
	if !c.HTTP && !c.STUN && !c.TCPSTUN {
		return fmt.Errorf("enable at least one of http, stun or tcp-stun")
	}
	if c.HTTP && c.Relay && !c.Signal {
		return fmt.Errorf("relay requires signal; disable both for directory-only mode")
	}
	if c.HTTP && c.State == "" {
		return fmt.Errorf("state path is required")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return fmt.Errorf("tls-cert and tls-key must be supplied together")
	}
	for _, origin := range split(c.Origins) {
		if !validOrigin(origin) {
			return fmt.Errorf("invalid browser origin: %q", origin)
		}
	}
	for _, raw := range append(split(c.PublicSTUN), split(c.SelfSTUN)...) {
		if !strings.HasPrefix(raw, "stun:") || strings.TrimPrefix(raw, "stun:") == "" {
			return fmt.Errorf("invalid STUN URL: %q", raw)
		}
	}
	for _, raw := range split(c.TCPSTUNPublic) {
		if _, _, err := net.SplitHostPort(raw); err != nil {
			return fmt.Errorf("invalid TCP probe endpoint: %q", raw)
		}
	}
	return nil
}
func split(value string) []string {
	out := []string{}
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Runtime owns all listeners, sockets and the SQLite database.
type Runtime struct {
	HTTPAddr  net.Addr
	UDPAddr   net.Addr
	TCPAddr   net.Addr
	http      *http.Server
	listener  net.Listener
	directory *deviceservice.Server
	udp       *connectivity.STUNServer
	tcp       *connectivity.TCPSTUNServer
	errors    chan error
}

func Start(c Config) (_ *Runtime, err error) {
	if err = c.Validate(); err != nil {
		return nil, err
	}
	r := &Runtime{errors: make(chan error, 1)}
	defer func() {
		if err != nil {
			_ = r.Close()
		}
	}()
	if c.HTTP {
		r.listener, err = net.Listen("tcp", c.Listen)
		if err != nil {
			return nil, fmt.Errorf("HTTP listen: %w", err)
		}
		r.HTTPAddr = r.listener.Addr()
	}
	if c.STUN {
		r.udp, err = connectivity.ListenSTUN(c.STUNListen)
		if err != nil {
			return nil, fmt.Errorf("UDP STUN listen: %w", err)
		}
		r.UDPAddr = r.udp.Addr()
	}
	if c.TCPSTUN {
		r.tcp, err = connectivity.ListenTCPSTUN(c.TCPSTUNListen)
		if err != nil {
			return nil, fmt.Errorf("TCP STUN listen: %w", err)
		}
		r.TCPAddr = r.tcp.Addr()
	}
	if c.HTTP {
		r.directory, err = deviceservice.New(c.State, split(c.Origins))
		if err != nil {
			return nil, err
		}
		r.directory.RelayEnabled = c.Relay
		r.directory.Network.PublicSTUN = split(c.PublicSTUN)
		r.directory.Network.SelfSTUN = split(c.SelfSTUN)
		r.directory.Network.TCPSTUN = split(c.TCPSTUNPublic)
		if r.UDPAddr != nil {
			_, r.directory.STUNPort, _ = net.SplitHostPort(r.UDPAddr.String())
		}
		if r.TCPAddr != nil {
			_, r.directory.TCPSTUNPort, _ = net.SplitHostPort(r.TCPAddr.String())
		}
		web := webui.Handler()
		if c.WebEnabled && c.Web != "" {
			web, err = webui.Directory(c.Web)
			if err != nil {
				return nil, err
			}
		}
		r.http = &http.Server{Handler: handler(c, r.directory, web), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 32 << 10}
		// ServeTLS validates the certificate before reporting successful startup.
		if c.TLSCert != "" {
			if err = configureTLS(r.http, c.TLSCert, c.TLSKey); err != nil {
				return nil, err
			}
		}
		go func() {
			var err error
			if c.TLSCert != "" {
				err = r.http.ServeTLS(r.listener, "", "")
			} else {
				err = r.http.Serve(r.listener)
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				r.errors <- err
			}
		}()
	}
	return r, nil
}

func handler(c Config, directory http.Handler, web http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		switch {
		case r.URL.Path == "/healthz":
			if r.Method != "GET" && r.Method != "HEAD" {
				w.WriteHeader(405)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, "ok")
		case r.URL.Path == "/api/account":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, `{"deployment":"self-hosted","account":null,"providers":[],"plans":[]}`)
		case strings.HasPrefix(r.URL.Path, "/p2p/"):
			if (r.URL.Path == "/p2p/signal" && !c.Signal) || (r.URL.Path == "/p2p/relay" && (!c.Signal || !c.Relay)) {
				http.NotFound(w, r)
				return
			}
			directory.ServeHTTP(w, r)
		case c.WebEnabled && (r.URL.Path == "/" || r.URL.Path == "/chat" || strings.HasPrefix(r.URL.Path, "/chat/") || strings.HasPrefix(r.URL.Path, "/assets/chat/")):
			web.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func (r *Runtime) Close() error {
	// Stop accepting new upgrades, then close hijacked sockets before closing SQLite.
	if r.listener != nil {
		_ = r.listener.Close()
	}
	if r.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := r.http.Shutdown(ctx)
		cancel()
		if err != nil {
			_ = r.http.Close()
		}
	}
	if r.directory != nil {
		_ = r.directory.Close()
	}
	if r.udp != nil {
		_ = r.udp.Close()
	}
	if r.tcp != nil {
		_ = r.tcp.Close()
	}
	return nil
}

func Run() {
	c, version, err := ParseConfig(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Print(err)
		os.Exit(2)
	}
	if version {
		fmt.Println("reignhub-server " + Version)
		return
	}
	r, err := Start(c)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	defer r.Close()
	log.Printf("ReignHub server %s; HTTP=%v UDP STUN=%v TCP STUN=%v", Version, r.HTTPAddr, r.UDPAddr, r.TCPAddr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-r.errors:
		log.Print(err)
	}
}
