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
	"github.com/monkeyWie/reignhub/internal/device"
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
	assets    *assets.Store
	probeMu   sync.Mutex
	probeAt   time.Time
	probeInfo cliDaemonInfo
	probeErr  error
	kind      string
	cfg       config.Config
	broker    *events.Broker
	mu        sync.Mutex
	client    *rpc.Client
	created   map[string]*rpc.Client
	endpoint  string
	shared    sharedRuntime
	lastError string
	pending   map[string]pending
	next      uint64
	connectMu sync.Mutex
	live      *liveStream
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

// Closing Hub disconnects only its own RPC client. Codex App connects directly
// to the independent official server and must keep working after Hub exits.
func (p *Provider) Close() error {
	p.mu.Lock()
	c := p.client
	p.client = nil
	p.created = nil
	p.mu.Unlock()
	if c != nil {
		_ = c.Close()
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
	connections, connectionErr := p.desktopConnected(ctx, instances)
	p.mu.Lock()
	defer p.mu.Unlock()
	s := provider.Status{ID: "codex-app", Name: "Codex App", State: "unavailable", Instances: instances, DesktopClients: connections, Endpoint: p.endpoint, LastError: p.lastError, Capabilities: provider.Capabilities{Streaming: true, History: true, Create: true, Send: true, Steer: true, Interrupt: true, Approvals: true, Desktop: true, ImageInput: true, FileInput: true}}
	s.Installed, s.Running, s.InstallPath = app != "", len(instances) > 0, app
	if err != nil {
		s.LastError = err.Error()
	}
	if appErr != nil {
		s.LastError = appErr.Error()
	}
	if !system.DesktopSupported {
		s.State = "unsupported"
		s.Detail = system.DesktopUnsupportedReason
		if appErr != nil {
			s.Detail = "安装状态检测失败；" + s.Detail
		} else if err != nil {
			s.Detail = "运行状态检测失败；" + s.Detail
		} else if app == "" {
			s.Detail = "未检测到 Codex 桌面应用；" + s.Detail
		}
		return s
	}
	if p.client == nil || !p.client.Alive() {
		s.Detail = "共享 app-server 未连接"
		if app == "" {
			s.Detail = "未检测到 Codex 桌面应用"
		} else if s.Running {
			s.Detail = "需重启接入；发现独立启动的 App"
		}
		return s
	}
	s.State = "awaitingActivation"
	s.Detail = "共享后端已就绪；接入官方 App 后即可远控"
	if len(instances) > 0 {
		s.State = "detected"
		s.Detail = "发现独立启动的 App；接入需正常退出后重新打开"
	}
	if connectionErr != nil {
		s.LastError = connectionErr.Error()
	}
	if connections > 0 {
		s.State = "connected"
		s.Detail = "已就绪，可以远控"
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
	UpdatedAt              int64 `json:"updatedAt"`
	Source                 json.RawMessage
	Originator             string
	Status                 struct {
		Type string `json:"type"`
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
	return provider.Session{ID: t.ID, ProviderID: "codex", Title: title, Preview: t.Preview, CWD: t.Cwd, UpdatedAt: t.UpdatedAt, Status: t.Status.Type, Source: string(t.Source), SourceKind: t.sourceKind(), Originator: t.Originator, Model: t.Model}
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
	page := provider.SessionPage{Sessions: []provider.Session{}, NextCursor: out.NextCursor}
	for _, t := range out.Data {
		s := p.session(t)
		if provider.SyncSession(s) {
			page.Sessions = append(page.Sessions, s)
		}
	}
	return page, nil
}
func (p *Provider) Transcript(ctx context.Context, id, cursor string) (provider.Transcript, error) {
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
		ID, Status             string
		Items                  []json.RawMessage
		StartedAt, CompletedAt *int64
	}
	_ = json.Unmarshal(raw, &t)
	out := provider.Turn{ID: t.ID, Status: t.Status, Items: []provider.Item{}}
	for _, raw := range t.Items {
		var i struct {
			ID, Type, Text, Command, AggregatedOutput, Phase, Status string
			DurationMS                                               *int64
			StartedAtMS, CompletedAtMS                               *int64
			Content                                                  json.RawMessage
			Summary                                                  []string
			Changes                                                  []struct {
				Path, Diff string
				Kind       json.RawMessage
			}
		}
		if json.Unmarshal(raw, &i) != nil {
			continue
		}
		item := provider.Item{ID: i.ID, Type: i.Type, Text: i.Text, Status: t.Status, Phase: i.Phase}
		switch i.Type {
		case "userMessage":
			item.Role = "user"
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
			item.Text = i.Command + "\n" + i.AggregatedOutput
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
		case "functionCallOutput", "hookPrompt", "contextCompaction", "enteredReviewMode", "exitedReviewMode":
			item.Role = "internal"
		default:
			item.Role = "tool"
			if item.Text == "" {
				item.Text = string(raw)
			}
		}
		if item.Role == "tool" {
			// A completed command can belong to a still-running turn. Missing
			// item status stays unknown rather than inheriting the turn status.
			item.Status = i.Status
			item.DurationMS = i.DurationMS
			item.Activities = toolActivities(raw)
		}
		// Keep recorded message times; turn boundaries are an explicit fallback
		// for older history without item timestamps, never the sync clock.
		if i.StartedAtMS != nil {
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
		if strings.TrimSpace(item.Text) != "" || len(item.Activities) > 0 || item.Role == "reasoning" || item.Role == "assistant" || (item.Role == "user" && len(i.Content) > 0) {
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
	if err := c.Call(ctx, "thread/start", params, &out); err != nil {
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
	c, err := p.threadClient(ctx, id)
	if err != nil {
		return err
	}
	params := map[string]any{"threadId": id, "input": []any{map[string]string{"type": "text", "text": text}}}
	if model != "" {
		params["model"] = model
	}
	return c.Call(ctx, "turn/start", params, nil)
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
	return p.call(ctx, "turn/steer", map[string]any{"threadId": id, "expectedTurnId": turn, "input": []any{map[string]string{"type": "text", "text": text}}}, nil)
}
func (p *Provider) Interrupt(ctx context.Context, id, turn string) error {
	return p.call(ctx, "turn/interrupt", map[string]string{"threadId": id, "turnId": turn}, nil)
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
	return nil
}
func (p *Provider) observe(m rpc.Message, reply func(context.Context, any) error) {
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
func (p *Provider) ConnectDesktop(ctx context.Context, expected []provider.Instance, confirm bool) error {
	if p.kind == "cli" {
		return p.Start(ctx)
	}
	if !system.DesktopSupported {
		return errors.New(system.DesktopUnsupportedReason)
	}
	if !p.connectMu.TryLock() {
		return errors.New("正在接入 App")
	}
	defer p.connectMu.Unlock()
	p.mu.Lock()
	alive := p.client != nil && p.client.Alive()
	endpoint := p.endpoint
	p.mu.Unlock()
	if !alive {
		return provider.ErrUnavailable
	}
	app := system.FindApp(p.cfg.CodexApp)
	current, err := system.Discover(ctx, app)
	if err != nil {
		return err
	}
	if connections, _ := p.desktopConnected(ctx, current); connections > 0 {
		return nil
	}
	if err := system.ConnectDesktop(ctx, app, endpoint, expected, confirm); err != nil {
		return err
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		current, err := system.Discover(ctx, app)
		if err != nil {
			return err
		}
		connections, err := system.DesktopConnections(ctx, current, endpoint)
		if err == nil && len(current) == 1 && connections > 0 {
			p.mu.Lock()
			state := p.shared
			state.Desktop = current
			p.shared = state
			p.mu.Unlock()
			if err := device.Save(p.runtimeFile(), state); err != nil {
				p.mu.Lock()
				p.shared.Desktop = nil
				p.mu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return errors.New("App 已启动，但尚未连接指定的共享 app-server；请检查 App 版本或 CODEX_APP_SERVER_FORCE_CLI 设置")
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
