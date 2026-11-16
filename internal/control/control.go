// Package control is the transport-independent application API.
package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monkeyWie/reignhub/internal/assets"
	"github.com/monkeyWie/reignhub/internal/provider"
)

type Request struct {
	Progress       bool                   `json:"progress,omitempty"`
	Probe          bool                   `json:"probe,omitempty"`
	Fresh          bool                   `json:"fresh,omitempty"`
	Params         json.RawMessage        `json:"params,omitempty"`
	Epoch          string                 `json:"epoch,omitempty"`
	After          int64                  `json:"after,omitempty"`
	ID             string                 `json:"id"`
	Revision       string                 `json:"revision,omitempty"`
	Method         string                 `json:"method"`
	Provider       string                 `json:"provider,omitempty"`
	Session        string                 `json:"session,omitempty"`
	Cursor         string                 `json:"cursor,omitempty"`
	Text           string                 `json:"text,omitempty"`
	Attachments    []string               `json:"attachments,omitempty"`
	Model          string                 `json:"model,omitempty"`
	Turn           string                 `json:"turn,omitempty"`
	Approval       string                 `json:"approval,omitempty"`
	Options        provider.CreateOptions `json:"options,omitempty"`
	Decision       provider.Decision      `json:"decision,omitempty"`
	Expected       []provider.Instance    `json:"expected,omitempty"`
	ConfirmRestart bool                   `json:"confirmRestart,omitempty"`
}

type Response struct {
	Partial     bool            `json:"partial,omitempty"`
	ID          string          `json:"id"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	Revision    string          `json:"revision,omitempty"`
	NotModified bool            `json:"notModified,omitempty"`
}

type Caller interface {
	Call(context.Context, Request, any) error
	Path() string
	Close() error
}
type PeerStatus struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type NetworkStatus struct {
	State  string       `json:"state"`
	Relay  string       `json:"relay,omitempty"`
	Detail string       `json:"detail,omitempty"`
	Peers  []PeerStatus `json:"peers"`
}
type Dispatcher struct {
	Assets     *assets.Store
	mutationMu [64]sync.Mutex
	Execute    func(Request, func() Response) Response
	Providers  map[string]provider.Provider
	Network    func() NetworkStatus
	Local      func(context.Context, Request) (any, error)
	Sync       func(context.Context, provider.Provider, Request) (any, error)
}

func (d *Dispatcher) Handle(ctx context.Context, r Request) Response {
	result, err := d.dispatch(ctx, r)
	out := Response{ID: r.ID}
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Result, err = json.Marshal(result)
	if err != nil {
		out.Error = err.Error()
	}
	if err == nil && ReadOnly(r.Method) {
		digest := sha256.Sum256(out.Result)
		out.Revision = fmt.Sprintf("%x", digest)
		if r.Revision == out.Revision {
			out.NotModified = true
			out.Result = nil
		}
	}
	return out
}
func (d *Dispatcher) HandleRemote(ctx context.Context, r Request) Response {
	if strings.HasPrefix(r.Method, "host.") || strings.HasPrefix(r.Method, "client.") || r.Method == "connectDesktop" {
		return Response{ID: r.ID, Error: "此操作仅允许在本机执行"}
	}
	if d.Execute != nil && !ReadOnly(r.Method) && r.Method != "watch" && r.Method != "network" && !strings.HasPrefix(r.Method, "attachment.") {
		digest := sha256.Sum256([]byte(r.Provider + "/" + r.Session))
		lock := &d.mutationMu[int(digest[0])%len(d.mutationMu)]
		lock.Lock()
		defer lock.Unlock()
		if err := ctx.Err(); err != nil {
			return Response{ID: r.ID, Error: err.Error()}
		}
		return d.Execute(r, func() Response { return d.Handle(ctx, r) })
	}
	return d.Handle(ctx, r)
}
func (d *Dispatcher) dispatch(ctx context.Context, r Request) (any, error) {
	if strings.HasPrefix(r.Method, "host.") || strings.HasPrefix(r.Method, "client.") {
		if d.Local == nil {
			return nil, fmt.Errorf("本机接口不可用")
		}
		return d.Local(ctx, r)
	}
	if r.Method == "network" {
		if d.Network != nil {
			return d.Network(), nil
		}
		return NetworkStatus{State: "disabled", Peers: []PeerStatus{}}, nil
	}
	if r.Method == "providers" {
		statuses := []provider.Status{}
		for _, p := range d.Providers {
			s := p.Status(ctx)
			s.Controllable = s.Ready()
			statuses = append(statuses, s)
		}
		sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
		return statuses, nil
	}
	p := d.Providers[r.Provider]
	if p == nil {
		return nil, fmt.Errorf("unknown provider %q", r.Provider)
	}
	if r.Method == "watch" {
		if r.Probe {
			if stream, ok := p.(provider.StreamProber); ok {
				return stream.Probe(ctx, r.Session, r.Epoch, r.After)
			}
		}
		if stream, ok := p.(provider.StreamingProvider); ok {
			return stream.Watch(ctx, r.Session, r.Epoch, r.After)
		}
		return nil, fmt.Errorf("此 Agent 暂不支持实时订阅")
	}
	if strings.HasPrefix(r.Method, "attachment.") {
		if d.Assets == nil {
			return nil, fmt.Errorf("附件服务不可用")
		}
		if _, ok := p.(provider.AttachmentSender); !ok {
			return nil, fmt.Errorf("此 Agent 暂不支持附件")
		}
		var params assets.Params
		if err := json.Unmarshal(r.Params, &params); err != nil {
			return nil, fmt.Errorf("无效附件参数")
		}
		return d.Assets.Handle(ctx, assets.Scope(r.Provider, r.Session), r.Method, params)
	}
	if r.Method != "connectDesktop" {
		s := p.Status(ctx)
		if !s.Ready() {
			return nil, fmt.Errorf("Agent 尚未就绪: %s", s.Detail)
		}
	}
	switch r.Method {
	case "sync":
		if d.Sync == nil {
			return nil, fmt.Errorf("同步不可用")
		}
		return d.Sync(ctx, p, r)
	case "sessions":
		return p.Sessions(ctx, r.Cursor)
	case "transcript":
		if !r.Progress {
			return p.Transcript(ctx, r.Session, r.Cursor)
		}
		parent, observed := ctx, false
		ctx = provider.WithTranscriptObserver(ctx, func(fragment provider.Transcript) error {
			observed = true
			return provider.ObserveTranscript(parent, fragment)
		})
		transcript, err := p.Transcript(ctx, r.Session, r.Cursor)
		if err == nil && !observed {
			for _, turn := range transcript.Turns {
				if err = provider.EmitTranscriptTurn(ctx, transcript, turn); err != nil {
					break
				}
			}
		}
		return transcript, err
	case "create":
		return p.Create(ctx, r.Options)
	case "send":
		if len(r.Attachments) > 0 {
			if sender, ok := p.(provider.AttachmentSender); ok {
				return nil, sender.SendAttachments(ctx, r.Session, r.Text, r.Model, r.Attachments)
			}
			return nil, fmt.Errorf("此 Agent 暂不支持附件")
		}
		if r.Model != "" {
			if models, ok := p.(provider.ModelProvider); ok {
				return nil, models.SendWithModel(ctx, r.Session, r.Text, r.Model)
			}
			return nil, fmt.Errorf("此 Agent 暂不支持模型切换")
		}
		return nil, p.Send(ctx, r.Session, r.Text)
	case "models":
		if models, ok := p.(provider.SessionModelProvider); ok && r.Session != "" {
			return models.SessionModels(ctx, r.Session)
		}
		if models, ok := p.(provider.ModelProvider); ok {
			return models.Models(ctx)
		}
		return []provider.Model{}, nil
	case "steer":
		if len(r.Attachments) > 0 {
			if sender, ok := p.(provider.AttachmentSender); ok {
				return nil, sender.SteerAttachments(ctx, r.Session, r.Turn, r.Text, r.Attachments)
			}
			return nil, fmt.Errorf("此 Agent 暂不支持附件")
		}
		return nil, p.Steer(ctx, r.Session, r.Turn, r.Text)
	case "interrupt":
		return nil, p.Interrupt(ctx, r.Session, r.Turn)
	case "approvals":
		return p.Approvals(), nil
	case "decide":
		return nil, p.Decide(ctx, r.Approval, r.Decision)
	case "connectDesktop":
		return nil, p.ConnectDesktop(ctx, r.Expected, r.ConfirmRestart)
	default:
		return nil, fmt.Errorf("unknown method %q", r.Method)
	}
}

// Only snapshots may be conditionally returned. Mutations always execute and
// are never cached or transparently retried.
func ReadOnly(method string) bool {
	switch method {
	case "providers", "network", "sessions", "transcript", "approvals", "sync", "models":
		return true
	}
	return false
}
