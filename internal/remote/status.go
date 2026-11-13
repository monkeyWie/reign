package remote

import (
	"sort"
	"sync"

	"github.com/monkeyWie/reign/internal/control"
)

// HostStatus reports authenticated live links, not a persistent device registry.
type HostStatus struct {
	mu                   sync.Mutex
	state, relay, detail string
	peers                map[string]*wire
}

func NewHostStatus(url string) *HostStatus {
	return &HostStatus{state: "connecting", relay: url, peers: make(map[string]*wire)}
}
func (s *HostStatus) ReportError(err error) { s.set("error", err.Error()) }
func (s *HostStatus) set(state, detail string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.state, s.detail = state, detail
	if state != "connected" {
		s.peers = make(map[string]*wire)
	}
	s.mu.Unlock()
}
func (s *HostStatus) track(id string, w *wire) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if w == nil {
		delete(s.peers, id)
	} else {
		s.peers[id] = w
	}
	s.mu.Unlock()
}
func (s *HostStatus) Snapshot() control.NetworkStatus {
	if s == nil {
		return control.NetworkStatus{State: "disabled", Peers: []control.PeerStatus{}}
	}
	s.mu.Lock()
	out := control.NetworkStatus{State: s.state, Relay: s.relay, Detail: s.detail, Peers: []control.PeerStatus{}}
	peers := make(map[string]*wire, len(s.peers))
	for id, w := range s.peers {
		peers[id] = w
	}
	s.mu.Unlock()
	for id, w := range peers {
		w.mu.Lock()
		authenticated := w.crypto != nil && w.ctx.Err() == nil
		w.mu.Unlock()
		if authenticated {
			out.Peers = append(out.Peers, control.PeerStatus{ID: id, Path: w.path()})
		}
	}
	sort.Slice(out.Peers, func(i, j int) bool { return out.Peers[i].ID < out.Peers[j].ID })
	return out
}

// DisconnectAll immediately closes data channels; the relay link stays available.
func (s *HostStatus) DisconnectAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	peers := make([]*wire, 0, len(s.peers))
	for _, w := range s.peers {
		peers = append(peers, w)
	}
	s.mu.Unlock()
	for _, w := range peers {
		w.close()
	}
}

// DisconnectDevice leaves other paired controllers connected.
func (s *HostStatus) DisconnectDevice(device string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	var peers []*wire
	for _, w := range s.peers {
		if w.device == device {
			peers = append(peers, w)
		}
	}
	s.mu.Unlock()
	for _, w := range peers {
		w.close()
	}
}
