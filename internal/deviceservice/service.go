// Package deviceservice provides the account-free device directory, pairing mailbox and relay.
package deviceservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/reign/internal/connectivity"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/relay"
	"github.com/monkeyWie/reign/pkg/database"
	"github.com/monkeyWie/reign/pkg/link"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Device struct {
	ID           string `json:"id" gorm:"primaryKey;size:64"`
	ConnectionID string `json:"connectionId" gorm:"uniqueIndex;size:32"`
	Name         string `json:"name"`
	Public       string `json:"public"`
	TokenHash    string `json:"tokenHash,omitempty" gorm:"uniqueIndex;size:64"`
	Host         bool   `json:"host"`
	Seen         int64  `json:"seen"`
	TunnelURL    string `json:"tunnelUrl,omitempty"`
}
type Pair struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Client   string `json:"client"`
	Name     string `json:"name"`
	Public   string `json:"public"`
	Envelope string `json:"envelope"`
	Reply    string `json:"reply,omitempty"`
	State    string `json:"state"`
	Expires  int64  `json:"expires"`
}
type DeviceGrant struct {
	Host   string `gorm:"primaryKey;size:64"`
	Client string `gorm:"primaryKey;size:64"`
}
type Metadata struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string
}
type bucket struct {
	At time.Time
	N  int
}
type Admission struct {
	Operation string
	DeviceID  string
	HostID    string
}
type Server struct {
	// Hooks are assigned before serving. Registration runs in the same database
	// transaction, so a rejected allocation cannot leave an unowned device.
	OnRegister  func(*gorm.DB, Device) error
	Policy      func(context.Context, Admission) error
	mu          sync.Mutex
	DB          *gorm.DB
	pairs       map[string]Pair
	rates       map[string]bucket
	relay       *relay.Server
	ServiceID   string
	Network     connectivity.Policy
	STUNPort    string
	TCPSTUNPort string
}

func hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func New(path string, origins []string) (*Server, error) {
	db, err := database.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	s, err := NewWithDB(db, origins)
	if err != nil {
		raw, _ := db.DB()
		_ = raw.Close()
	}
	return s, err
}

// NewWithDB composes the directory with any configured GORM database. The
// caller owns the database lifetime. SQLite remains the self-hosted default.
func NewWithDB(db *gorm.DB, origins []string) (*Server, error) {
	if err := db.AutoMigrate(&Device{}, &DeviceGrant{}, &Metadata{}); err != nil {
		return nil, err
	}
	meta := Metadata{Key: "service_id", Value: device.Random()}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&meta).Error; err != nil {
		return nil, err
	}
	if err := db.First(&meta, "key = ?", "service_id").Error; err != nil {
		return nil, err
	}
	s := &Server{DB: db, pairs: map[string]Pair{}, rates: map[string]bucket{}, Network: connectivity.DefaultPolicy(), ServiceID: meta.Value}
	s.relay = relay.New("")
	s.relay.Origins = origins
	s.relay.AuthorizeContext = func(ctx context.Context, token, host, role string) (string, bool) {
		id := s.identity(token)
		if id == "" {
			return "", false
		}
		var target Device
		if s.DB.First(&target, "id = ? AND host = ?", host, true).Error != nil {
			return "", false
		}
		if s.Policy != nil && s.Policy(ctx, Admission{Operation: "relay." + role, DeviceID: id, HostID: host}) != nil {
			return "", false
		}
		if role == "host" {
			return id, id == host
		}
		var grant DeviceGrant
		return id, role == "client" && s.DB.First(&grant, "host = ? AND client = ?", host, id).Error == nil
	}
	return s, nil
}
func (s *Server) Close() error {
	raw, err := s.DB.DB()
	if err != nil {
		return err
	}
	return raw.Close()
}
func (s *Server) identity(token string) string {
	if token == "" {
		return ""
	}
	var d Device
	if s.DB.First(&d, "token_hash = ?", hash(token)).Error != nil {
		return ""
	}
	return d.ID
}
func (s *Server) allow(k string, limit int) bool {
	now := time.Now()
	if len(s.rates) > 20000 {
		for key, v := range s.rates {
			if now.Sub(v.At) > time.Minute {
				delete(s.rates, key)
			}
		}
		if _, ok := s.rates[k]; !ok && len(s.rates) > 20000 {
			return false
		}
	}
	b := s.rates[k]
	if now.Sub(b.At) > time.Minute {
		b = bucket{At: now}
	}
	b.N++
	s.rates[k] = b
	return b.N <= limit
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/capabilities" && r.Method == "GET" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{"protocolVersion": 2, "deployment": "self-hosted", "accountRequired": false, "billingEnabled": false, "managedTunnel": false})
		return
	}
	if r.URL.Path == "/relay" {
		s.relay.ServeHTTP(w, r)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		allowed := origin == "http://"+r.Host || origin == "https://"+r.Host
		for _, o := range s.relay.Origins {
			if match, _ := path.Match(o, origin); match {
				allowed = true
			}
		}
		if !allowed {
			http.Error(w, "origin denied", 403)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	}
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var in struct {
		Name, Public, ConnectionID, Host, ID, Envelope, Reply, Client, TunnelURL string
		IsHost                                                                   bool
		Allow                                                                    bool
	}
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); e != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.allow("ip:"+ip, 300) {
		http.Error(w, "rate limited", 429)
		return
	}
	now := time.Now().Unix()
	for id, p := range s.pairs {
		if p.Expires < now {
			delete(s.pairs, id)
		}
	}
	write := func(v any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(v) }
	if r.URL.Path == "/v2/network" {
		policy := s.Network
		if len(policy.SelfSTUN) == 0 {
			if endpoint := connectivity.STUNURL(r.Host, s.STUNPort); endpoint != "" {
				policy.SelfSTUN = []string{endpoint}
			}
		}
		if len(policy.TCPSTUN) == 0 && s.TCPSTUNPort != "" {
			if endpoint := connectivity.STUNURL(r.Host, s.TCPSTUNPort); endpoint != "" {
				policy.TCPSTUN = []string{strings.TrimPrefix(endpoint, "stun:")}
			}
		}
		write(policy)
		return
	}
	if r.URL.Path == "/v2/register" {
		var count int64
		if s.DB.Model(&Device{}).Count(&count).Error != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		if count >= 10000 || !s.allow("register:"+ip, 10) {
			http.Error(w, "registration limit", 429)
			return
		}
		raw, e := hex.DecodeString(in.Public)
		if e != nil || len(raw) != 32 || len(in.Name) > 100 {
			http.Error(w, "invalid identity", 400)
			return
		}
		token := device.Random()
		id := relay.ID()
		var code string
		for {
			n, e := rand.Int(rand.Reader, big.NewInt(1_000_000))
			if e != nil {
				http.Error(w, "device ID allocation failed", 503)
				return
			}
			code = fmt.Sprintf("%06d", n.Int64())
			var count int64
			if s.DB.Model(&Device{}).Where("connection_id = ?", code).Count(&count).Error != nil {
				http.Error(w, "database unavailable", 503)
				return
			}
			if count == 0 {
				break
			}
		}
		d := Device{ID: id, ConnectionID: code, Name: in.Name, Public: in.Public, TokenHash: hash(token), Host: in.IsHost, Seen: now}
		if e = s.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
			if s.OnRegister != nil {
				return s.OnRegister(tx, d)
			}
			return nil
		}); e != nil {
			http.Error(w, "device registration failed", 503)
			return
		}

		write(map[string]any{"id": id, "connectionId": code, "token": token, "serviceId": s.ServiceID})
		return
	}
	id := s.identity(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if id == "" {
		http.Error(w, "unauthorized", 401)
		return
	}
	var me Device
	if s.DB.First(&me, "id = ?", id).Error != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	if s.Policy != nil {
		if err := s.Policy(r.Context(), Admission{Operation: r.URL.Path, DeviceID: id, HostID: in.Host}); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	switch r.URL.Path {
	case "/v2/presence":
		if in.Name != "" && len(in.Name) <= 100 {
			me.Name = in.Name
		}
		if me.Host {
			endpoint, err := link.PublicEndpoint(in.TunnelURL)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			me.TunnelURL = endpoint
		}
		me.Seen = now
		if e := s.DB.Save(&me).Error; e != nil {
			http.Error(w, "persistence failed", 500)
			return
		}
		write(map[string]bool{"ok": true})
	case "/v2/resolve":
		code := strings.ReplaceAll(strings.ReplaceAll(in.ConnectionID, " ", ""), "-", "")
		var d Device
		if s.DB.First(&d, "connection_id = ? AND host = ?", code, true).Error != nil {
			http.Error(w, "device not found", 404)
			return
		}
		d.TokenHash = ""
		// A tunnel endpoint is disclosed only to an already paired controller.
		var grant DeviceGrant
		if s.DB.First(&grant, "host = ? AND client = ?", d.ID, id).Error != nil {
			d.TunnelURL = ""
		}
		write(map[string]any{"device": d, "online": now-d.Seen < 45})

	case "/v2/pair/request":
		var host Device
		err := s.DB.First(&host, "id = ?", in.Host).Error
		if err != nil || !host.Host || len(in.Envelope) > 32000 || len(in.ID) != 32 {
			http.Error(w, "invalid pairing target", 400)
			return
		}
		if !s.allow("pair:"+id, 10) || !s.allow("hostpair:"+host.ID, 30) || len(s.pairs) > 1000 {
			http.Error(w, "pairing rate limited", 429)
			return
		}
		if _, exists := s.pairs[in.ID]; exists {
			http.Error(w, "duplicate request", 409)
			return
		}
		p := Pair{ID: in.ID, Host: host.ID, Client: id, Name: me.Name, Public: me.Public, Envelope: in.Envelope, State: "pending", Expires: now + 120}
		s.pairs[p.ID] = p
		write(p)
	case "/v2/pair/pending":
		out := []Pair{}
		for _, p := range s.pairs {
			if p.Host == id && p.State == "pending" {
				out = append(out, p)
			}
		}
		write(out)
	case "/v2/pair/result":
		p, ok := s.pairs[in.ID]
		if !ok || p.Client != id {
			http.Error(w, "pairing expired", 404)
			return
		}
		write(p)
	case "/v2/pair/respond":
		p, ok := s.pairs[in.ID]
		if !ok || p.Host != id || p.State != "pending" {
			http.Error(w, "request unavailable", 409)
			return
		}
		p.State = "denied"
		if in.Allow {
			if in.Reply == "" {
				http.Error(w, "missing encrypted reply", 400)
				return
			}
			if e := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&DeviceGrant{Host: p.Host, Client: p.Client}).Error; e != nil {
				http.Error(w, "persistence failed", 500)
				return
			}
			p.State = "approved"
			p.Reply = in.Reply
		}
		s.pairs[p.ID] = p
		write(p)
	case "/v2/revoke":
		if e := s.DB.Delete(&DeviceGrant{}, "host = ? AND client = ?", id, in.Client).Error; e != nil {
			http.Error(w, "persistence failed", 500)
			return
		}
		s.relay.DisconnectDevice(id, in.Client)
		write(map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

// Authenticate resolves a device credential without exposing its stored token hash.
func (s *Server) Authenticate(token string) (Device, bool) {
	id := s.identity(token)
	var d Device
	if id == "" || s.DB.First(&d, "id = ?", id).Error != nil {
		return d, false
	}
	d.TokenHash = ""
	return d, true
}

// CanAccess checks core device authorization independently of cloud identity.
func (s *Server) CanAccess(deviceID, hostID string) bool {
	var host Device
	if s.DB.First(&host, "id = ? AND host = ?", hostID, true).Error != nil {
		return false
	}
	if deviceID == hostID {
		return true
	}
	var grant DeviceGrant
	return s.DB.First(&grant, "host = ? AND client = ?", hostID, deviceID).Error == nil
}
