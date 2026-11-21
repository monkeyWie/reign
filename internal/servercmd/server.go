// Package servercmd runs the standalone, account-free ReignHub service.
package servercmd

import (
	"context"

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
	"github.com/monkeyWie/reignhub/internal/nativepath"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/monkeyWie/reignhub/internal/webui"
)

// Version is set by release builds.
var Version = "dev"

type Config struct {
	ExternalRelays []relay.NodeConfig              `json:"externalRelays"`
	RelayControl   string                          `json:"relayControl"`
	ExternalSTUN   connectivity.TransportAddresses `json:"externalStun"`
	HTTP           bool                            `json:"http"`
	API            bool                            `json:"api"`
	Signal         bool                            `json:"signal"`
	Relay          bool                            `json:"relay"`
	WebEnabled     bool                            `json:"webEnabled"`
	STUN           bool                            `json:"stun"`
	TCPSTUN        bool                            `json:"tcpStun"`
	Listen         string                          `json:"listen"`
	State          string                          `json:"state"`
	Web            string                          `json:"web"`
	STUNListen     string                          `json:"stunListen"`
	TCPSTUNListen  string                          `json:"tcpStunListen"`
	PublicSTUN     string                          `json:"publicStun"`
	SelfSTUN       string                          `json:"selfStun"`
	TCPSTUNPublic  string                          `json:"tcpStunPublic"`
	Origins        string                          `json:"origins"`
	NativeListen   string                          `json:"nativeRelayListen"`
	NativePublic   string                          `json:"nativeRelayPublic"`
	TLSCert        string                          `json:"tlsCert"`
	TLSKey         string                          `json:"tlsKey"`
}

func DefaultConfig() Config {
	return Config{ExternalRelays: []relay.NodeConfig{}, ExternalSTUN: connectivity.TransportAddresses{}, HTTP: true, API: true, Signal: true, Relay: true, WebEnabled: true, STUN: true, TCPSTUN: true,
		NativeListen: ":26332", Listen: ":26330", State: "data/reignhub.db", STUNListen: ":" + connectivity.DefaultSTUNPort, TCPSTUNListen: ":" + connectivity.DefaultSTUNPort,
		PublicSTUN: strings.Join(connectivity.DefaultPolicy().PublicSTUN, ",")}
}

func (c Config) Validate() error {
	if _, _, err := c.resolveNodes(); err != nil {
		return err
	}
	return c.validateListeners()
}
func (c Config) resolveNodes() (connectivity.Endpoints, []nativepath.Endpoint, error) {
	stun, err := c.ExternalSTUN.Resolve()
	if err != nil {
		return stun, nil, fmt.Errorf("externalStun: %w", err)
	}
	nodes, err := relay.ResolveNodes(c.ExternalRelays)
	if err != nil {
		return stun, nil, fmt.Errorf("externalRelays: %w", err)
	}
	return stun, nodes, nil
}
func (c Config) validateListeners() error {
	if c.RelayControl != "" && (!relay.ValidControlOrigin(c.RelayControl) || c.NativeListen == "" || c.State == "") {
		return errors.New("relayControl requires an HTTPS origin (HTTP allowed on loopback), state and nativeRelayListen")
	}
	if !c.HTTP && !c.STUN && !c.TCPSTUN && c.RelayControl == "" {
		return fmt.Errorf("enable at least one HTTP, discovery or external relay listener")
	}
	if c.HTTP && c.Relay && !c.Signal {
		return fmt.Errorf("relay requires signal; disable both for directory-only mode")
	}
	if c.HTTP && c.Signal && !c.API {
		return fmt.Errorf("signal requires the device API")
	}
	if c.HTTP && !c.WebEnabled && !c.API {
		return fmt.Errorf("HTTP requires the device API or Web console")
	}
	if c.HTTP && c.API && c.State == "" {
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
	if c.NativePublic != "" {
		host, port, err := net.SplitHostPort(c.NativePublic)
		if err != nil || host == "" || port == "" {
			return errors.New("invalid native relay public host:port")
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
	native    *relay.NativeListener
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
	stun, nodes, err := c.resolveNodes()
	if err != nil {
		return nil, err
	}
	if err = c.validateListeners(); err != nil {
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
	if (c.HTTP && c.API) || c.RelayControl != "" {
		r.directory, err = deviceservice.New(c.State, split(c.Origins))
		if err != nil {
			return nil, err
		}
		r.directory.RelayEnabled = c.Relay
		r.directory.ConfigureNative(nodes, c.RelayControl)
		r.directory.ExternalSTUN = stun
		if (c.RelayControl != "" || c.HTTP && c.Signal && c.Relay) && c.NativeListen != "" {
			r.native, err = r.directory.ListenNative(c.NativeListen, c.NativePublic)
			if err != nil {
				return nil, fmt.Errorf("native relay listen: %w", err)
			}
		}
		r.directory.Network.PublicSTUN = split(c.PublicSTUN)
		r.directory.Network.SelfSTUN = split(c.SelfSTUN)
		r.directory.Network.TCPSTUN = split(c.TCPSTUNPublic)
		if r.UDPAddr != nil {
			_, r.directory.STUNPort, _ = net.SplitHostPort(r.UDPAddr.String())
		}
		if r.TCPAddr != nil {
			_, r.directory.TCPSTUNPort, _ = net.SplitHostPort(r.TCPAddr.String())
		}
	}
	if c.HTTP {
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
		case c.API && r.URL.Path == "/api/account":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, `{"deployment":"self-hosted","account":null,"providers":[],"plans":[]}`)
		case c.API && strings.HasPrefix(r.URL.Path, "/p2p/"):
			if (r.URL.Path == "/p2p/signal" && !c.Signal) || ((r.URL.Path == "/p2p/relay" || r.URL.Path == relay.NativeLeasePath) && (!c.Signal || !c.Relay)) {
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
	if r.native != nil {
		r.native.Close()
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
	cmd, err := parseCommand(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Print(err)
		os.Exit(2)
	}
	switch cmd.action {
	case printVersion:
		fmt.Println("reignhub-server " + Version)
		return
	case initConfig:
		if err := writeDefaultConfig(cmd.file); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		fmt.Println("Created", cmd.file)
		return
	}
	c, err := loadConfig(cmd.file)
	if err != nil {
		log.Print(err)
		os.Exit(2)
	}
	if cmd.action == printRelayPin {
		pin, err := deviceservice.ReadRelayPin(c.State)
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		fmt.Println(pin)
		return
	}
	r, err := Start(c)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	defer r.Close()
	var nativeAddr net.Addr
	if r.native != nil {
		nativeAddr = r.native.Addr()
	}
	log.Printf("ReignHub server %s; HTTP=%v UDP STUN=%v TCP STUN=%v native relay=%v", Version, r.HTTPAddr, r.UDPAddr, r.TCPAddr, nativeAddr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-r.errors:
		log.Print(err)
	}
}
