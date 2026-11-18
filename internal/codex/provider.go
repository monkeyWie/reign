package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/assets"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/rpc"
	"github.com/monkeyWie/reignhub/internal/system"
)

type pending struct {
	approval         provider.Approval
	reply            func(context.Context, any) error
	route, requestID string
}
type Provider struct {
	assets          *assets.Store
	probeMu         sync.Mutex
	probeAt         time.Time
	probeInfo       cliDaemonInfo
	probeErr        error
	kind            string
	cfg             config.Config
	broker          *events.Broker
	mu              sync.Mutex
	client          *rpc.Client
	created         map[string]*rpc.Client
	endpoint        string
	ipc             *appIPC
	lastError       string
	pending         map[string]pending
	next            uint64
	connectMu       sync.Mutex
	live            *liveStream
	projectMu       sync.Mutex
	projectsAt      time.Time
	projects        *projectIndex
	recencies       map[string]sessionRecencyEntry
	recencySequence uint64
}

func New(c config.Config, _ string, b *events.Broker, stores ...*assets.Store) *Provider {
	store := assets.New(filepath.Join(c.DataDir, "attachments"))
	if len(stores) > 0 {
		store = stores[0]
	}
	return &Provider{cfg: c, broker: b, pending: make(map[string]pending), assets: store}
}
func NewCLI(c config.Config, b *events.Broker, stores ...*assets.Store) *Provider {
	p := New(c, "", b, stores...)
	p.kind = "cli"
	return p
}
func (p *Provider) Start(ctx context.Context) error {
	if p.kind == "cli" {
		return p.startCLI(ctx, true)
	}
	if !system.DesktopSupported {
		return errors.New(system.DesktopUnsupportedReason)
	}
	return p.startAppServer(ctx)
}

// Closing Hub releases only its own executor and IPC identity. It never
// terminates, relaunches, or rewrites the environment of the user's Codex App.
func (p *Provider) Close() error {
	p.connectMu.Lock()
	defer p.connectMu.Unlock()
	p.mu.Lock()
	c, b := p.client, p.ipc
	p.client = nil
	p.ipc = nil
	p.created = nil
	p.pending = map[string]pending{}
	p.mu.Unlock()
	if c != nil {
		_ = c.Close()
		select {
		case <-c.Done():
		case <-time.After(2 * time.Second):
		}
	}
	if b != nil {
		b.close()
	}
	p.clearRoute("service")
	return nil
}
func (p *Provider) Status(ctx context.Context) provider.Status {
	if p.kind == "cli" {
		return p.cliStatus(ctx)
	}
	app, appErr := system.FindAppContext(ctx, p.cfg.CodexApp)
	instances, err := system.Discover(ctx, app)
	p.mu.Lock()
	c, b, last := p.client, p.ipc, p.lastError
	p.mu.Unlock()
	s := provider.Status{ID: "codex-app", Name: "Codex App", State: "unavailable", Installed: app != "", Running: len(instances) > 0, InstallPath: app, Instances: instances, LastError: last, Capabilities: provider.Capabilities{SessionRemoval: "archive", Streaming: true, History: true, Create: true, Send: true, Steer: true, Interrupt: true, Approvals: true, Desktop: true, ImageInput: true, FileInput: true}}
	if appErr != nil {
		s.LastError = appErr.Error()
	}
	if err != nil {
		s.LastError = err.Error()
	}
	if !system.DesktopSupported {
		s.State = "unsupported"
		s.Detail = system.DesktopUnsupportedReason
		return s
	}
	s.Detail = "打开 Codex App 后自动连接"
	if s.Running {
		s.State = "detected"
		s.Detail = "正在连接"
	}
	if b != nil {
		ipc := b.client()
		if s.Running && ipc != nil && ipc.Alive() && c != nil && c.Alive() {
			s.State = "connected"
			s.Detail = "已就绪"
			s.Endpoint = "ipc"
			s.DesktopClients = 1
		}
	}
	return s
}
func (p *Provider) call(ctx context.Context, method string, params, out any) error {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()
	if c == nil || !c.Alive() {
		return provider.ErrUnavailable
	}
	return c.Call(ctx, method, params, out)
}

type thread struct {
	ID, Name, Preview, Cwd string
	Model                  string
	ProjectID              string `json:"projectId"`
	UpdatedAt              int64  `json:"updatedAt"`
	CreatedAt              int64  `json:"createdAt"`
	Source                 json.RawMessage
	Originator             string
	Status                 struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	}
	Turns []json.RawMessage
}

func (t thread) session() provider.Session {
	title := t.Name
	if title == "" {
		title = strings.ReplaceAll(t.Preview, "\n", " ")
		if len([]rune(title)) > 70 {
			title = string([]rune(title)[:70]) + "…"
		}
	}
	if title == "" {
		title = "未命名会话"
	}
	preview := []rune(t.Preview)
	if len(preview) > 240 {
		preview = append(preview[:240], '…')
	}
	status := t.Status.Type
	if status == "active" && len(t.Status.ActiveFlags) > 0 {
		status = "waiting"
	}
	return provider.Session{ID: t.ID, ProviderID: "codex", Title: title, Preview: string(preview), CWD: t.Cwd, UpdatedAt: provider.TimestampMillis(t.UpdatedAt), CreatedAt: provider.TimestampMillis(t.CreatedAt), Status: status, Source: string(t.Source), SourceKind: t.sourceKind(), Originator: t.Originator, Model: t.Model}
}
func (p *Provider) Sessions(ctx context.Context, cursor string) (provider.SessionPage, error) {
	params := map[string]any{"limit": 50, "sortKey": "updated_at", "sortDirection": "desc", "sourceKinds": []string{"cli", "vscode", "appServer", "subAgent", "unknown"}, "modelProviders": []string{}}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var out struct {
		Data       []thread
		NextCursor string
	}
	if err := p.call(ctx, "thread/list", params, &out); err != nil {
		return provider.SessionPage{}, err
	}
	p.refreshProjects(ctx)
	page := provider.SessionPage{Sessions: []provider.Session{}, NextCursor: out.NextCursor}
	for _, t := range out.Data {
		s := p.session(t)
		if provider.SyncSession(s) {
			page.Sessions = append(page.Sessions, s)
		}
	}
	p.enrichSessionActivity(ctx, page.Sessions)
	p.enrichSessionRecency(ctx, page.Sessions)
	return page, nil
}
func (p *Provider) TurnStatus(ctx context.Context, session, turn string) (provider.Turn, error) {
	var out struct{ Data []struct{ ID, Status string } }
	err := p.call(ctx, "thread/turns/list", map[string]any{"threadId": session, "limit": 30, "sortDirection": "desc", "itemsView": "full", "itemSnapshot": "latest"}, &out)
	if err != nil {
		return provider.Turn{}, err
	}
	for _, item := range out.Data {
		if item.ID == turn {
			return provider.Turn{ID: turn, Status: item.Status}, nil
		}
	}
	return provider.Turn{ID: turn}, nil
}

func (p *Provider) Transcript(ctx context.Context, id, cursor string) (provider.Transcript, error) {
	if out, ok, err := p.ipcTranscript(ctx, id, cursor); ok {
		if err == nil || strings.HasPrefix(cursor, "ipc:") || ctx.Err() != nil {
			return out, err
		}
		// A broken follower must not hide persisted history. These app-server
		// APIs only read; they neither resume the thread nor acquire its writer.
	}
	// Capture before reading: later events can safely replay over this snapshot.
	epoch, boundary := p.stream().log.Position()
	var metadata struct{ Thread thread }
	if err := p.call(ctx, "thread/read", map[string]any{"threadId": id}, &metadata); err != nil {
		return provider.Transcript{}, err
	}
	params := map[string]any{"threadId": id, "limit": 30, "sortDirection": "desc", "itemsView": "full", "itemSnapshot": "latest"}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var turns struct {
		Data       []json.RawMessage
		NextCursor string
	}
	if err := p.call(ctx, "thread/turns/list", params, &turns); err != nil {
		// Codex does not materialize a new thread's history until its first
		// user message. Metadata remains readable in the creating connection.
		var rpcErr *rpc.Error
		if cursor != "" || !p.isCreatedThread(id) || !errors.As(err, &rpcErr) || rpcErr.Code != -32600 || !strings.Contains(rpcErr.Message, "is not materialized yet") {
			return provider.Transcript{}, err
		}
	}
	out := provider.Transcript{StreamEpoch: epoch, StreamCursor: boundary, Session: p.session(metadata.Thread), Turns: []provider.Turn{}, NextCursor: turns.NextCursor}
	if err := provider.ObserveTranscript(ctx, out); err != nil {
		return provider.Transcript{}, err
	}
	for i := len(turns.Data) - 1; i >= 0; i-- {
		normalized := normalizeTurn(turns.Data[i])
		p.attachments(ctx, id, metadata.Thread.Cwd, &normalized, turns.Data[i])
		out.Turns = append(out.Turns, normalized)
		out.Session.RecencyAt = max(out.Session.RecencyAt, provider.ConversationRecency([]provider.Turn{normalized}))
		if err := provider.EmitTranscriptTurn(ctx, out, normalized); err != nil {
			return provider.Transcript{}, err
		}
	}
	// Historical turns may retain inProgress after a client restart or a
	// superseding turn. Only the newest turn on the latest page is actionable.
	if cursor == "" && len(out.Turns) > 0 {
		latest := out.Turns[len(out.Turns)-1]
		if latest.Status == "inProgress" {
			out.Session.ActiveTurnID = latest.ID
		}
	}
	return out, nil
}
func normalizeTurn(raw json.RawMessage) provider.Turn {
	var t struct {
		ID, Status                 string
		Items                      []json.RawMessage
		StartedAt, CompletedAt     *int64
		StartedAtMS, CompletedAtMS *int64
		DurationMS                 *int64
	}
	_ = json.Unmarshal(raw, &t)
	out := provider.Turn{ID: t.ID, Status: t.Status, DurationMS: turnDuration(t.DurationMS, t.StartedAt, t.CompletedAt), Items: []provider.Item{}}
	out.RecencyAt = recordedTurnRecency(t.Status, t.StartedAt, t.CompletedAt, t.StartedAtMS, t.CompletedAtMS, t.DurationMS)
	for _, raw := range t.Items {
		var i struct {
			ID, Type, Text, Phase, Status string
			DurationMS                    *int64
			StartedAtMS, CompletedAtMS    *int64
			Content, Input                json.RawMessage
			RestoreMessage                struct{ CreatedAt *int64 }
			Summary                       []string
			Changes                       []struct {
				Path, Diff string
				Kind       json.RawMessage
			}
		}
		if json.Unmarshal(raw, &i) != nil {
			continue
		}
		item := provider.Item{ID: i.ID, Type: i.Type, Text: i.Text, Status: t.Status, Phase: i.Phase}
		switch i.Type {
		case "userMessage", "steeringUserMessage":
			item.Role = "user"
			if i.Type == "steeringUserMessage" {
				i.Content = i.Input
				item.Status = i.Status
			}
			var content []struct{ Type, Text string }
			if len(i.Content) > 0 && json.Unmarshal(i.Content, &content) != nil {
				continue
			}
			for _, c := range content {
				if c.Text != "" {
					item.Text += c.Text + "\n"
				} else if c.Type != "localImage" && c.Type != "image" {
					item.Text += "[" + c.Type + "]\n"
				}
			}
		case "agentMessage":
			item.Role = "assistant"
		case "commandExecution":
			item.Role = "tool"
		case "reasoning":
			item.Role = "reasoning"
			// Only the protocol's display summary is presentation metadata.
			// reasoning.content has a different shape from user input and must
			// never be used as a fallback for the visible generation status.
			item.Text = ""
			for _, summary := range i.Summary {
				if summary = strings.TrimSpace(summary); summary != "" {
					item.ReasoningSummary = summary
				}
			}
		case "functionCallOutput", "hookPrompt", "contextCompaction", "enteredReviewMode", "exitedReviewMode", "steered":
			item.Role = "internal"
			item.Text = ""
		default:
			item.Role = "tool"
		}
		if item.Role == "tool" {
			// The controller renders typed activities, never raw tool stdout or
			// embedded native JSON. Do not put megabytes of invisible output in
			// front of the next assistant message during ordered catch-up.
			item.Text = ""
			// A completed command can belong to a still-running turn. Missing
			// item status stays unknown rather than inheriting the turn status.
			item.Status = i.Status
			item.DurationMS = i.DurationMS
			item.Activities = toolActivities(raw)
		}
		// Keep recorded message times; turn boundaries are an explicit fallback
		// for older history without item timestamps, never the sync clock.
		if i.Type == "steeringUserMessage" && i.RestoreMessage.CreatedAt != nil {
			item.TimestampMS = i.RestoreMessage.CreatedAt
		} else if i.StartedAtMS != nil {
			item.TimestampMS = i.StartedAtMS
		} else if i.CompletedAtMS != nil {
			item.TimestampMS = i.CompletedAtMS
		} else if item.Role == "assistant" && i.Phase != "commentary" && t.CompletedAt != nil {
			ms := *t.CompletedAt * 1000
			item.TimestampMS, item.TimestampSource = &ms, "turnCompleted"
		} else if t.StartedAt != nil {
			ms := *t.StartedAt * 1000
			item.TimestampMS, item.TimestampSource = &ms, "turnStarted"
		}
		if strings.TrimSpace(item.Text) != "" || len(item.Activities) > 0 || item.Role == "tool" || item.Role == "reasoning" || item.Role == "assistant" || (item.Role == "user" && len(i.Content) > 0) {
			out.Items = append(out.Items, item)
		}
	}
	return out
}
func (p *Provider) Create(ctx context.Context, o provider.CreateOptions) (provider.Session, error) {
	params := map[string]any{}
	if o.CWD != "" {
		params["cwd"] = o.CWD
	}
	if o.Model != "" {
		params["model"] = o.Model
	}
	var out struct{ Thread thread }
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()
	if c == nil || !c.Alive() {
		return provider.Session{}, provider.ErrUnavailable
	}
	var createdRaw json.RawMessage
	if err := c.Call(ctx, "thread/start", params, &createdRaw); err != nil {
		return provider.Session{}, err
	}
	if err := json.Unmarshal(createdRaw, &out); err != nil {
		return provider.Session{}, err
	}
	if out.Thread.ID == "" {
		return provider.Session{}, errors.New("Codex did not return a thread ID")
	}
	p.mu.Lock()
	if p.client == c {
		if p.created == nil {
			p.created = make(map[string]*rpc.Client)
		}
		p.created[out.Thread.ID] = c
	}
	p.mu.Unlock()
	if b := p.appBridge(); b != nil {
		b.register(out.Thread.ID, createdRaw, true)
	}
	return p.session(out.Thread), nil
}

// Only threads created on the current transport can skip resume. Persisted
// threads still resume normally, preserving writer-conflict checks and IDs.
func (p *Provider) isCreatedThread(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.client != nil && p.created[id] == p.client && p.client.Alive()
}
func (p *Provider) threadClient(ctx context.Context, id string) (*rpc.Client, error) {
	p.mu.Lock()
	c := p.client
	created := c != nil && p.created[id] == c
	p.mu.Unlock()
	if c == nil || !c.Alive() {
		return nil, provider.ErrUnavailable
	}
	if !created {
		var resumed json.RawMessage
		if err := c.Call(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &resumed); err != nil {
			return nil, fmt.Errorf("无法接续原会话（不会另建会话）: %w", err)
		}
		if b := p.appBridge(); b != nil {
			b.register(id, resumed, false)
			p.mu.Lock()
			if p.client == c {
				if p.created == nil {
					p.created = map[string]*rpc.Client{}
				}
				p.created[id] = c
			}
			p.mu.Unlock()
		}
	}
	return c, nil
}

func (p *Provider) Send(ctx context.Context, id, text string) error {
	return p.SendWithModel(ctx, id, text, "")
}
func (p *Provider) SendWithModel(ctx context.Context, id, text, model string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("消息不能为空")
	}
	return p.sendInput(ctx, id, []any{map[string]string{"type": "text", "text": text}}, model)
}

func (p *Provider) Models(ctx context.Context) ([]provider.Model, error) {
	models := []provider.Model{}
	cursor := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Data []struct {
				Model, DisplayName string
				IsDefault, Hidden  bool
			}
			NextCursor string
		}
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := p.call(ctx, "model/list", params, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Data {
			if !m.Hidden && m.Model != "" {
				models = append(models, provider.Model{ID: m.Model, Name: m.DisplayName, Default: m.IsDefault})
			}
		}
		if page.NextCursor == "" {
			return models, nil
		}
		if seen[page.NextCursor] {
			return nil, errors.New("model/list returned a repeated cursor")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}
func (p *Provider) Steer(ctx context.Context, id, turn, text string) error {
	return p.steerInput(ctx, id, turn, []any{map[string]string{"type": "text", "text": text}})
}
func (p *Provider) Interrupt(ctx context.Context, id, turn string) error {
	c, owner, err := p.writeRoute(ctx, id)
	if err != nil {
		return err
	}
	if owner != "" {
		b := p.appBridge()
		if b == nil {
			return provider.ErrUnavailable
		}
		_, err = b.request(ctx, "thread-follower-interrupt-turn", map[string]any{"conversationId": id, "mode": "user-stop", "expectedTurnId": turn}, owner)
		return err
	}
	return c.Call(ctx, "turn/interrupt", map[string]string{"threadId": id, "turnId": turn}, nil)
}
func (p *Provider) Approvals() []provider.Approval {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []provider.Approval{}
	for _, a := range p.pending {
		out = append(out, a.approval)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
func (p *Provider) addPending(m rpc.Message, reply func(context.Context, any) error, route string) string {
	var meta struct{ ThreadID, TurnID string }
	_ = json.Unmarshal(m.Params, &meta)
	p.mu.Lock()
	p.next++
	id := fmt.Sprint(p.next)
	a := provider.Approval{ID: id, ProviderID: "codex", SessionID: meta.ThreadID, TurnID: meta.TurnID, Method: m.Method, Params: m.Params, CreatedAt: time.Now()}
	p.pending[id] = pending{approval: a, reply: reply, route: route, requestID: string(m.ID)}
	p.mu.Unlock()
	p.broker.Publish("codex", "approval", a)
	return id
}
func (p *Provider) removePending(id string) { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }
func (p *Provider) Decide(ctx context.Context, id string, d provider.Decision) error {
	p.mu.Lock()
	a, ok := p.pending[id]
	p.mu.Unlock()
	if !ok {
		return errors.New("审批已处理或已过期")
	}
	var result any
	switch a.approval.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if d.Decision != "accept" && d.Decision != "acceptForSession" && d.Decision != "decline" && d.Decision != "cancel" {
			return errors.New("无效审批决定")
		}
		result = map[string]string{"decision": d.Decision}
	case "item/tool/requestUserInput":
		result = map[string]any{"answers": d.Answers}
	default:
		return fmt.Errorf("此请求须在官方 App 处理: %s", a.approval.Method)
	}
	// Claim once before writing: concurrent remote clients must not answer twice.
	p.mu.Lock()
	_, ok = p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.mu.Unlock()
	if !ok {
		return errors.New("审批已处理或已过期")
	}
	if err := a.reply(ctx, result); err != nil {
		return err
	}
	p.removePending(id)
	if b := p.appBridge(); b != nil {
		b.markDirty(a.approval.SessionID)
	}
	return nil
}
func (p *Provider) observe(m rpc.Message, reply func(context.Context, any) error) {
	defer func() {
		if b := p.appBridge(); b != nil {
			b.observe(m)
		}
	}()
	if len(m.ID) > 0 {
		p.addPending(m, reply, "service")
	} else {
		if m.Method == "thread/closed" {
			var closed struct{ ThreadID string }
			if json.Unmarshal(m.Params, &closed) == nil {
				p.mu.Lock()
				delete(p.created, closed.ThreadID)
				p.mu.Unlock()
			}
		}
		p.observeStream(m)
		p.resolvePending("service", m)
		p.broker.Publish("codex", m.Method, m.Params)
	}
}

// IPC connects to the already-running App. expected/confirm are obsolete
// launch controls and are deliberately never used to restart another program.
func (p *Provider) ConnectDesktop(ctx context.Context, _ []provider.Instance, _ bool) error {
	return p.Start(ctx)
}

func (p *Provider) clearRoute(route string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if route == "service" && p.live != nil {
		p.live.mu.Lock()
		p.live.items = map[string]*liveItem{}
		p.live.log.Reset()
		p.live.mu.Unlock()
	}
	for id, a := range p.pending {
		if a.route == route {
			delete(p.pending, id)
		}
	}
}
func (p *Provider) resolvePending(route string, m rpc.Message) {
	if m.Method != "serverRequest/resolved" && m.Method != "turn/completed" && m.Method != "thread/closed" {
		return
	}
	var meta struct {
		ThreadID  string
		RequestID json.RawMessage
		Turn      struct{ ID string }
	}
	if json.Unmarshal(m.Params, &meta) != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, a := range p.pending {
		resolved := m.Method == "serverRequest/resolved" && a.route == route && a.requestID == string(meta.RequestID)
		completed := m.Method == "turn/completed" && meta.Turn.ID != "" && a.approval.SessionID == meta.ThreadID && a.approval.TurnID == meta.Turn.ID
		closed := m.Method == "thread/closed" && a.route == route && a.approval.SessionID == meta.ThreadID
		if resolved || completed || closed {
			delete(p.pending, id)
		}
	}
}
