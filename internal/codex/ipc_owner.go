package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func (b *appIPC) Handle(ctx context.Context, m codexipc.Message) (any, error) {
	if !b.CanHandle(m) {
		return nil, errors.New("no-client-found")
	}
	id, _ := ipcID(m.Params)
	params := decodeObject(m.Params)
	var result any
	var err error
	switch m.Method {
	case "thread-owner-discovery":
		result = map[string]any{}
	case "thread-follower-start-turn":
		request := object(object(params["turnStart"])["request"])
		if stringValue(request["threadId"]) != id {
			return nil, errors.New("thread ID mismatch")
		}
		native := map[string]any{"threadId": id}
		for _, key := range []string{"input", "model", "effort", "serviceTier", "collaborationMode", "approvalPolicy", "approvalsReviewer", "sandboxPolicy", "clientUserMessageId", "cwd", "outputSchema"} {
			if v := request[key]; v != nil {
				native[key] = v
			}
		}
		if len(array(native["input"])) == 0 {
			return nil, errors.New("missing turn input")
		}
		var out json.RawMessage
		err = b.startFromApp(ctx, id, native, &out)
		result = out
	case "thread-follower-steer-turn":
		b.mu.Lock()
		turn := activeOwnedTurn(b.owned[id])
		b.mu.Unlock()
		if turn == "" {
			return nil, errors.New("no active turn")
		}
		var out json.RawMessage
		err = b.executeFromApp(ctx, id, "turn/steer", map[string]any{"threadId": id, "expectedTurnId": turn, "input": params["input"], "clientUserMessageId": params["clientUserMessageId"]}, &out)
		result = out
	case "thread-follower-interrupt-turn":
		b.mu.Lock()
		turn := activeOwnedTurn(b.owned[id])
		b.mu.Unlock()
		expected := stringValue(params["expectedTurnId"])
		if expected != "" && turn != "" && expected != turn {
			return nil, errors.New("active turn changed")
		}
		if turn != "" {
			err = b.p.call(ctx, "turn/interrupt", map[string]any{"threadId": id, "turnId": turn}, nil)
		}
		result = map[string]any{"interruptedTurnId": turn, "ok": true}
	case "thread-follower-load-complete-history":
		b.mu.Lock()
		if o := b.owned[id]; o != nil {
			o.followers[m.Source] = true
		}
		b.mu.Unlock()
		err = b.publish(ctx, id, true)
		b.mu.Lock()
		var revision int64
		if o := b.owned[id]; o != nil {
			revision = o.revision
		}
		b.mu.Unlock()
		result = map[string]any{"revision": revision}
	case "thread-follower-read-model-settings":
		b.mu.Lock()
		o := b.owned[id]
		if o != nil {
			result = map[string]any{"settings": map[string]any{"model": o.settings["model"], "reasoningEffort": o.settings["reasoningEffort"], "resumeState": "resumed", "mode": "default", "threadSource": "user", "modelProvider": o.settings["modelProvider"]}}
		}
		b.mu.Unlock()
	case "thread-follower-compact-thread":
		err = b.p.call(ctx, "thread/compact/start", map[string]any{"threadId": id}, nil)
		result = map[string]any{"ok": err == nil}
	case "thread-follower-update-thread-settings":
		native := decodeObject(rawJSON(object(params["threadSettings"])))
		if native == nil {
			return nil, errors.New("invalid thread settings")
		}
		b.mu.Lock()
		o := b.owned[id]
		active := activeOwnedTurn(o)
		condition := object(params["condition"])
		matched := o != nil && (condition == nil || (reflect.DeepEqual(condition["ifEffortEquals"], o.settings["reasoningEffort"]) && (condition["ifModelEquals"] == nil || condition["ifModelEquals"] == o.settings["model"])))
		b.mu.Unlock()
		if !matched {
			return map[string]any{"method": m.Method, "result": map[string]any{"applied": false}}, nil
		}
		if expected := stringValue(params["activeTurnId"]); expected != "" && expected != active {
			return nil, errors.New("active turn changed")
		}
		native["threadId"] = id
		// The IPC snapshot uses sandboxPolicy while thread/settings/update expects
		// named permissions on newer servers. Do not combine the two encodings.
		if native["permissions"] != nil && native["sandboxPolicy"] != nil {
			return nil, errors.New("ambiguous permissions")
		}
		err = b.p.call(ctx, "thread/settings/update", native, nil)
		if err == nil {
			b.mu.Lock()
			if o := b.owned[id]; o != nil {
				previous := object(o.settings["threadSettings"])
				if previous == nil {
					previous = map[string]any{}
				}
				for key, value := range native {
					if key != "threadId" {
						previous[key] = value
					}
				}
				o.settings["threadSettings"] = previous
				if v, exists := native["effort"]; exists {
					o.settings["reasoningEffort"] = v
				}
				if v := native["model"]; v != nil {
					o.settings["model"] = v
				}
			}
			b.mu.Unlock()
			b.markDirty(id)
		}
		result = map[string]any{"applied": err == nil}
	case "thread-follower-command-approval-decision", "thread-follower-file-approval-decision", "thread-follower-submit-user-input", "thread-follower-permissions-request-approval-response", "thread-follower-submit-mcp-server-elicitation-response":
		err = b.answerOwnerApproval(ctx, id, m.Method, params)
		result = map[string]any{"ok": err == nil}
	case "thread-follower-set-queued-follow-ups-state":
		messages := array(object(params["state"])[id])
		if len(messages) > 100 {
			return nil, errors.New("too many queued messages")
		}
		discarded := map[string]bool{}
		for _, v := range array(params["discardedMessageIds"]) {
			discarded[stringValue(v)] = true
		}
		queue := []map[string]any{}
		seen := map[string]bool{}
		for _, value := range messages {
			message := object(value)
			key := stringValue(message["id"])
			if key == "" || seen[key] {
				return nil, errors.New("invalid queued message ID")
			}
			seen[key] = true
			if discarded[key] {
				continue
			}
			if _, err = queuedInput(message); err != nil {
				return nil, err
			}
			queue = append(queue, decodeObject(rawJSON(message)))
		}
		b.mu.Lock()
		if o := b.owned[id]; o != nil {
			filtered := []map[string]any{}
			for _, message := range queue {
				key := stringValue(message["id"])
				outcome := o.sends[key]
				if !o.sent[key] && o.inflight != key && (outcome == nil || !outcome.uncertain) {
					filtered = append(filtered, message)
				}
			}
			o.queue = filtered
		}
		b.mu.Unlock()
		b.queueChanged(id)
		go b.drainQueue(id)
		result = map[string]any{"ok": true}
	case "thread-follower-remove-queued-message", "thread-follower-clear-queued-messages":
		b.mu.Lock()
		o := b.owned[id]
		removed := false
		cleared := []map[string]any{}
		if o != nil {
			kept := []map[string]any{}
			for _, message := range o.queue {
				if m.Method == "thread-follower-clear-queued-messages" || message["id"] == params["messageId"] {
					cleared = append(cleared, message)
					if message["id"] == o.blocked {
						o.blocked = ""
						o.uncertain = false
					}
					removed = true
				} else {
					kept = append(kept, message)
				}
			}
			o.queue = kept
		}
		b.mu.Unlock()
		b.queueChanged(id)
		go b.drainQueue(id)
		if m.Method == "thread-follower-clear-queued-messages" {
			result = map[string]any{"messages": cleared}
		} else {
			result = map[string]any{"removed": removed}
		}
	default:
		return nil, errors.New("no-handler-for-request")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"method": m.Method, "result": result}, nil
}
func activeOwnedTurn(o *ownedThread) string {
	if o == nil || len(o.turns) == 0 {
		return ""
	}
	t := o.turns[len(o.turns)-1]
	if t["status"] == "inProgress" {
		return stringValue(t["id"])
	}
	return ""
}

func (b *appIPC) answerOwnerApproval(ctx context.Context, id, method string, params map[string]any) error {
	response := method == "thread-follower-submit-user-input" || method == "thread-follower-permissions-request-approval-response" || method == "thread-follower-submit-mcp-server-elicitation-response"
	if response && object(params["response"]) == nil {
		return errors.New("missing approval response")
	}
	if !response {
		if !validIPCDecision(params["decision"]) {
			return errors.New("invalid approval decision")
		}
	}
	key := string(rawJSON(params["requestId"]))
	b.p.mu.Lock()
	var reply func(context.Context, any) error
	for token, p := range b.p.pending {
		if p.route == "service" && p.approval.SessionID == id && p.requestID == key {
			want := map[string]string{"item/commandExecution/requestApproval": "thread-follower-command-approval-decision", "item/fileChange/requestApproval": "thread-follower-file-approval-decision", "item/tool/requestUserInput": "thread-follower-submit-user-input", "item/permissions/requestApproval": "thread-follower-permissions-request-approval-response", "mcpServer/elicitation/request": "thread-follower-submit-mcp-server-elicitation-response"}[p.approval.Method]
			if want != method {
				continue
			}
			reply = p.reply
			delete(b.p.pending, token)
			break
		}
	}
	b.p.mu.Unlock()
	if reply == nil {
		return errors.New("approval already resolved")
	}
	var payload any = map[string]any{"decision": params["decision"]}
	if response {
		payload = params["response"]

	}
	err := reply(ctx, payload)
	b.markDirty(id)
	return err
}
func queuedInput(message map[string]any) ([]any, error) {
	text := stringValue(message["text"])
	input := []any{}
	if strings.TrimSpace(text) != "" {
		input = append(input, map[string]any{"type": "text", "text": text})
	}
	context := object(message["context"])
	// Preserve local image inputs. Reject unfamiliar attachment shapes instead
	// of silently sending text with the user's files missing.
	if value := context["attachments"]; value != nil {
		if _, ok := value.([]any); !ok {
			return nil, errors.New("unsupported queued attachment structure")
		}
	}
	for _, value := range array(context["attachments"]) {
		attachment := object(value)
		path := stringValue(attachment["path"])
		if path == "" {
			path = stringValue(attachment["filePath"])
		}
		if !filepath.IsAbs(path) {
			return nil, errors.New("queued attachment shape is unsupported; send it from its original window")
		}
		kind := stringValue(attachment["type"])
		mime := stringValue(attachment["mimeType"])
		if kind == "localImage" || kind == "image" || strings.HasPrefix(mime, "image/") {
			input = append(input, map[string]any{"type": "localImage", "path": path})
		} else {
			input = append(input, map[string]any{"type": "text", "text": fmt.Sprintf("Attached file: %s", path)})
		}
	}
	if len(array(context["commentAttachments"])) > 0 || len(array(context["mcpAppModelContextAttachments"])) > 0 {
		return nil, errors.New("queued rich context is unsupported")
	}
	if len(input) == 0 {
		return nil, errors.New("queued message is empty")
	}
	return input, nil
}
func (b *appIPC) queueChanged(id string) {
	b.mu.Lock()
	o := b.owned[id]
	messages := []map[string]any{}
	if o != nil {
		messages = append(messages, o.queue...)
	}
	b.mu.Unlock()
	ctx, stop := context.WithTimeout(b.ctx, 3*time.Second)
	defer stop()
	_ = b.broadcast(ctx, "thread-queued-followups-changed", map[string]any{"hostId": "local", "conversationId": id, "messages": messages}, nil)
}
func (b *appIPC) drainQueue(id string) {
	b.mu.Lock()
	o := b.owned[id]
	if o == nil || o.draining || o.uncertain || len(o.queue) == 0 || activeOwnedTurn(o) != "" {
		b.mu.Unlock()
		return
	}
	message := o.queue[0]
	if message["pausedReason"] != nil {
		b.mu.Unlock()
		return
	}
	if len(o.sent) >= 4096 {
		b.mu.Unlock()
		return
	}
	o.draining = true
	o.inflight = stringValue(message["id"])
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		o.draining = false
		o.inflight = ""
		again := !o.uncertain && len(o.queue) > 0 && activeOwnedTurn(o) == ""
		b.mu.Unlock()
		if again {
			go b.drainQueue(id)
		}
	}()
	input, err := queuedInput(message)
	ctx, stop := context.WithTimeout(b.ctx, 20*time.Second)
	defer stop()
	if err == nil {
		params := map[string]any{"threadId": id, "input": input, "clientUserMessageId": message["id"]}
		options := object(object(message["context"])["submissionOptions"])
		for _, key := range []string{"model", "effort", "collaborationMode", "serviceTier"} {
			if v := options[key]; v != nil {
				params[key] = v
			}
		}
		var out struct{ Turn map[string]any }
		var raw json.RawMessage
		err = b.startFromApp(ctx, id, params, &raw)
		if err == nil {
			err = json.Unmarshal(raw, &out)
		}
		if err == nil {
			b.mu.Lock()
			if len(out.Turn) > 0 {
				found := false
				for _, t := range o.turns {
					if t["id"] == out.Turn["id"] {
						found = true
						break
					}
				}
				if !found {
					o.turns = append(o.turns, out.Turn)
				}
			}
			if o.sent == nil {
				o.sent = map[string]bool{}
			}
			o.sent[stringValue(message["id"])] = true
			kept := []map[string]any{}
			for _, m := range o.queue {
				if m["id"] != message["id"] {
					kept = append(kept, m)
				}
			}
			o.queue = kept
			b.mu.Unlock()
			b.queueChanged(id)
			b.markDirty(id)
			return
		}
	}
	// Even a definite failure waits for an explicit user retry; a lost response
	// must never replay a command merely because a new snapshot arrived.
	b.mu.Lock()
	o.uncertain = true
	o.blocked = stringValue(message["id"])
	b.mu.Unlock()
	b.p.mu.Lock()
	b.p.lastError = "Codex App 排队消息未确认，保留队列且不自动重发: " + err.Error()
	b.p.mu.Unlock()
}

// The App supplies a stable message ID. Keep a bounded outcome ledger so a
// repeated IPC request cannot execute a turn twice after an acknowledgement loss.
func (b *appIPC) startFromApp(ctx context.Context, id string, params map[string]any, out *json.RawMessage) error {
	return b.executeFromApp(ctx, id, "turn/start", params, out)
}
func (b *appIPC) executeFromApp(ctx context.Context, id, method string, params map[string]any, out *json.RawMessage) error {
	key := stringValue(params["clientUserMessageId"])
	if key == "" {
		return errors.New("missing client message ID")
	}
	b.mu.Lock()
	o := b.owned[id]
	if o == nil {
		b.mu.Unlock()
		return errors.New("no-client-found")
	}
	if o.sends == nil {
		o.sends = map[string]*ownedSend{}
	}
	fingerprint := method + ":" + string(rawJSON(params))
	if previous := o.sends[key]; previous != nil {
		if previous.fingerprint != fingerprint {
			b.mu.Unlock()
			return errors.New("message ID reused with different input")
		}
		if previous.pending || previous.uncertain {
			b.mu.Unlock()
			return &codexipc.UnknownOutcome{Method: method, Err: errors.New("message outcome pending; reconcile before retry")}
		}
		*out = append(json.RawMessage(nil), previous.result...)
		b.mu.Unlock()
		return nil
	}
	if len(o.sends) >= 4096 {
		b.mu.Unlock()
		return errors.New("message outcome ledger full; reopen this session")
	}
	entry := &ownedSend{pending: true, fingerprint: fingerprint}
	o.sends[key] = entry
	b.mu.Unlock()
	err := b.p.call(ctx, method, params, out)
	b.mu.Lock()
	defer b.mu.Unlock()
	entry.pending = false
	if err != nil {
		var remote *rpc.Error
		if errors.As(err, &remote) {
			delete(o.sends, key)
		} else {
			entry.uncertain = true
			return &codexipc.UnknownOutcome{Method: method, Err: err}
		}
		return err
	}
	entry.result = append(json.RawMessage(nil), (*out)...)
	b.markDirty(id)
	if o.sent == nil {
		o.sent = map[string]bool{}
	}
	o.sent[key] = true
	var response struct{ Turn map[string]any }
	_ = json.Unmarshal(*out, &response)
	if len(response.Turn) > 0 {
		found := false
		for _, t := range o.turns {
			found = found || t["id"] == response.Turn["id"]
		}
		if !found {
			o.turns = append(o.turns, response.Turn)
		}
	}
	return nil
}

// Codex approval decisions include structured policy amendments. Forward only
// recognized variants; the native executor validates each amendment's contents.
func validIPCDecision(value any) bool {
	switch stringValue(value) {
	case "accept", "acceptForSession", "decline", "cancel":
		return true
	}
	decision := object(value)
	if len(decision) != 1 {
		return false
	}
	for key, value := range decision {
		if key != "acceptWithExecpolicyAmendment" && key != "acceptWithNetworkPolicyAmendment" {
			return false
		}
		return object(value) != nil
	}
	return false
}
