package agents

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/provider"
)

func (p *Provider) permission(ctx context.Context, sessionID, title string, options []permissionOption) (string, error) {
	return p.requestPermission(ctx, sessionID, title, options, nil)
}
func (p *Provider) requestPermission(ctx context.Context, sessionID, title string, options []permissionOption, initial *string) (string, error) {
	id := device.Random()[:24]
	params, _ := json.Marshal(map[string]any{"title": title, "options": options, "input": initial != nil, "value": initial})
	p.mu.Lock()
	s := p.sessions[sessionID]
	turn := ""
	if s != nil {
		turn = s.turn
	}
	pending := &pendingApproval{approval: provider.Approval{ID: id, ProviderID: p.id, SessionID: sessionID, TurnID: turn, Method: title, Params: params, CreatedAt: time.Now()}, options: options, input: initial != nil, answer: make(chan string, 1)}
	p.approvals[id] = pending
	p.mu.Unlock()
	p.log.Publish(provider.StreamEvent{SessionID: sessionID, TurnID: turn, Kind: "approval"})
	defer func() {
		p.mu.Lock()
		delete(p.approvals, id)
		p.mu.Unlock()
		p.log.Publish(provider.StreamEvent{SessionID: sessionID, Kind: "approval"})
	}()
	select {
	case selected := <-pending.answer:
		return selected, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.life.Done():
		return "", p.life.Err()
	}
}
func (p *Provider) Approvals() []provider.Approval {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []provider.Approval{}
	for _, a := range p.approvals {
		out = append(out, a.approval)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
func (p *Provider) Decide(ctx context.Context, id string, decision provider.Decision) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	pending := p.approvals[id]
	if pending == nil || pending.answered {
		return errors.New("授权请求已结束")
	}
	choice := decision.Decision
	if pending.input {
		if choice != "input" {
			return errors.New("请填写 Agent 请求的内容")
		}
		values := decision.Answers["value"].Answers
		if len(values) != 1 || len(values[0]) > 64<<10 {
			return errors.New("输入内容无效或超过 64 KiB")
		}
		choice = values[0]
		select {
		case pending.answer <- choice:
			pending.answered = true
			return nil
		default:
			return errors.New("授权请求已回答")
		}
	}
	// Legacy buttons choose only a one-time outcome, never a persistent grant.
	for _, option := range pending.options {
		if choice == "accept" && option.Kind == "allow_once" || choice == "decline" && option.Kind == "reject_once" {
			choice = option.ID
			break
		}
	}
	valid := false
	for _, option := range pending.options {
		if option.ID == choice {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("请选择 Agent 提供的授权选项")
	}
	select {
	case pending.answer <- choice:
		pending.answered = true
		return nil
	default:
		return errors.New("授权请求已回答")
	}
}
