package hubui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Demo data never opens a real Provider or sends instructions to an Agent.
type demoAPI struct {
	mu         sync.Mutex
	sessions   []Object
	transcript Object
}

func newDemoAPI() *demoAPI {
	now := time.Now().UnixMilli()
	s := Object{"id": "demo-chat", "providerId": "codex-app", "title": "ReignHub 界面与连接方案", "cwd": "/workspace/agenthub", "updatedAt": now, "createdAt": now, "sourceKind": "app", "model": "gpt-5.4"}
	return &demoAPI{sessions: []Object{s, {"id": "demo-cli", "providerId": "codex-cli", "title": "检查客户端的同步状态", "cwd": "/workspace/agenthub", "updatedAt": now - 1000, "createdAt": now - 1000, "sourceKind": "cli"}, {"id": "demo-claude", "providerId": "claude-code", "title": "整理组件与主题", "cwd": "/workspace/design-system", "updatedAt": now - 2000, "createdAt": now - 2000, "sourceKind": "custom"}}, transcript: Object{"session": s, "turns": []Object{{"id": "demo-turn", "status": "completed", "items": []Object{{"id": "demo-user", "role": "user", "text": "统一聚合这些会话，按照工作目录分组。\n\n桌面和手机都要保持一致的体验。", "timestampMs": now - 20000}, {"id": "demo-assistant", "role": "assistant", "text": "已按工作目录聚合会话，项目中的会话按创建时间排序，项目下方的最近区按发送消息或 Agent 回复完成时间展示全部未置顶会话。\n\n## 连接方式\n\n- 优先尝试 **WebRTC 直连**\n- 不可直连时使用自己的 WSS 中继\n- 断线后保留本地消息缓存\n\n```go\nfunc main() {\n    nsgo.Run(hubui.Root)\n}\n```\n\n| 平台 | UI |\n| --- | --- |\n| 桌面 / 手机 | 原生视图 |\n| 浏览器 | Go WASM + DOM |", "timestampMs": now - 10000}}}}}}
}
func (*demoAPI) CanHost() bool { return true }
func (*demoAPI) Close()        {}
func (d *demoAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (result any, err error) {
	if method == "client.call" && str(r, "method") == "watch" {
		if flag(r, "probe") {
			return Object{"epoch": "demo", "cursor": number(r, "after"), "reset": str(r, "epoch") != "demo"}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
			return Object{"epoch": "demo", "cursor": 1, "heartbeat": true}, nil
		}
	}
	if target == "local" {
		if method == "client.cached" {
			return Object{"providers": demoProviders(), "sessions": []Object{}}, nil
		}
		if method == "client.sync" {
			return []Object{}, nil
		}
		if method == "client.call" && str(r, "method") == "sessions" {
			return Object{"sessions": []Object{}}, nil
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if result != nil {
			b, _ := json.Marshal(result)
			_ = json.Unmarshal(b, &result)
		}
	}()
	switch method {
	case "host.bootstrap", "host.configure", "host.password", "host.respond", "host.revoke", "host.enable", "host.activate", "client.disconnect", "client.forget":
		return nil, nil
	case "host.info":
		return Object{"name": "我的 MacBook Pro", "connectionId": "266556", "deviceId": "demo-local", "serviceUrl": DefaultService, "status": "在线", "passwordEnabled": true, "pending": []Object{}, "grants": []Object{}, "qr": Object{"service": DefaultService, "connectionId": "266556", "public": "demo"}}, nil
	case "providers":
		return demoProviders(), nil
	case "client.list", "client.connect", "client.reconnect":
		return []Object{{"id": "demo-device", "name": "工作电脑", "connectionId": "123456", "path": "P2P · WebRTC", "connected": true, "autoReconnect": false, "service": DefaultService}}, nil
	case "client.cached":
		if str(r, "method") == "transcript" {
			return d.transcript, nil
		}
		if str(r, "method") == "models" {
			return []Object{{"id": "gpt-5.4", "name": "GPT-5.4"}, {"id": "gpt-5.3-codex", "name": "GPT-5.3 Codex"}}, nil
		}
		return Object{"providers": demoProviders(), "sessions": d.sessions}, nil
	case "client.sync":
		out := []Object{}
		for _, s := range d.sessions {
			if str(s, "providerId") == str(p, "provider") {
				out = append(out, s)
			}
		}
		return out, nil
	case "client.assetCache.get":
		return nil, nil
	case "client.assetCache.put", "client.assetCache.clear":
		return nil, nil
	case "client.call":
		switch str(r, "method") {
		case "providers":
			return demoProviders(), nil
		case "sessions":
			out := []Object{}
			for _, session := range d.sessions {
				if str(session, "providerId") == str(r, "provider") {
					out = append(out, session)
				}
			}
			return Object{"sessions": out}, nil
		case "transcript":
			return d.transcript, nil
		case "session.remove":
			kept := []Object{}
			for _, ss := range d.sessions {
				if str(ss, "id") != str(r, "session") || str(ss, "providerId") != str(r, "provider") {
					kept = append(kept, ss)
				}
			}
			d.sessions = kept
			return nil, nil
		case "models":
			return []Object{{"id": "gpt-5.4", "name": "GPT-5.4"}, {"id": "gpt-5.3-codex", "name": "GPT-5.3 Codex"}}, nil
		case "approvals":
			return []Object{}, nil
		case "send", "steer":
			turn := Object{"id": fmt.Sprint("demo-", time.Now().UnixNano()), "status": "completed", "items": []Object{{"id": "prompt", "role": "user", "text": str(r, "text"), "timestampMs": time.Now().UnixMilli()}, {"id": "reply", "role": "assistant", "text": "这是隔离演示环境的回复，没有向任何真实 Agent 发送指令。", "timestampMs": time.Now().UnixMilli()}}}
			d.transcript["turns"] = append(objects(d.transcript["turns"]), turn)
			return nil, nil
		case "interrupt", "decide":
			return nil, nil
		case "create":
			s := Object{"id": fmt.Sprint("new-", time.Now().UnixNano()), "providerId": str(r, "provider"), "title": "新会话", "cwd": str(object(r["options"]), "cwd"), "updatedAt": time.Now().UnixMilli(), "createdAt": time.Now().UnixMilli()}
			d.sessions = append(d.sessions, s)
			return s, nil
		}
	}
	return nil, fmt.Errorf("演示环境不提供 %s / %s", method, str(r, "method"))
}
func demoProviders() []Object {
	return []Object{{"id": "codex-app", "name": "Codex App", "installed": true, "state": "ready", "detail": "已就绪", "capabilities": Object{"sessionRemoval": "archive", "desktop": true, "history": true, "create": true, "imageInput": true, "fileInput": true}}, {"id": "codex-cli", "name": "Codex CLI", "installed": true, "state": "ready", "detail": "已就绪", "capabilities": Object{"sessionRemoval": "archive", "history": true, "create": true}}, {"id": "claude-code", "name": "Claude Code", "installed": true, "state": "ready", "detail": "已就绪", "capabilities": Object{"sessionRemoval": "archive", "history": true}}}
}
