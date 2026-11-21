//go:build (darwin && !ios) || windows || (linux && !android)

package hubui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	model "github.com/monkeyWie/reignhub/internal/terminal"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type terminalUITestEngine struct {
	mu    sync.Mutex
	dirty bool
	input string
	done  chan struct{}
	once  sync.Once
}

type terminalUIFaultAPI struct {
	API
	failRead       atomic.Bool
	attachFailures atomic.Int32
}

func (a *terminalUIFaultAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" {
		if str(r, "method") == "terminal.read" && a.failRead.CompareAndSwap(true, false) {
			return nil, errors.New("fixture link lost")
		}
		if str(r, "method") == "terminal.attach" && a.attachFailures.Load() > 0 {
			a.attachFailures.Add(-1)
			return nil, errors.New("fixture reconnect temporarily unavailable")
		}
	}
	return a.API.Call(ctx, method, p, target, r)
}

func TestTerminalPanelTransientReconnect(t *testing.T) {
	e := &terminalUITestEngine{dirty: true, done: make(chan struct{})}
	var starts atomic.Int32
	m := model.New(context.Background(), func(context.Context, string, model.Size, string) (model.Engine, error) { starts.Add(1); return e, nil })
	t.Cleanup(func() { _ = m.Close() })
	d := &control.Dispatcher{Terminals: m, Providers: map[string]provider.Provider{"pi": terminalUITestProvider{dir: t.TempDir()}}}
	var api *terminalUIFaultAPI
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := workspaceLayoutFixture(cx)
		s.providers = []Object{{"id": "pi", "capabilities": Object{"terminal": true}}}
		api = &terminalUIFaultAPI{API: &terminalUITestAPI{s.api, d}}
		s.api = api
		return s.workspaceView(cx)
	}, nsgotest.Size(1200, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.Press("id:terminal-toggle")
	app.WaitFor("id:conversation-terminal", 4*time.Second)
	api.attachFailures.Store(2)
	api.failRead.Store(true)
	e.Send(nil)
	app.WaitFor("id:terminal-error", 2*time.Second)
	app.WaitUntil(12*time.Second, func() bool { return app.Exists("id:conversation-terminal") && !app.Exists("id:terminal-error") })
	if starts.Load() != 1 || api.attachFailures.Load() != 0 {
		t.Fatal("reconnect recreated the shell or stopped at first setup failure")
	}
	app.Key("z")
	app.WaitUntil(time.Second, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.input == "z" })
}

func (e *terminalUITestEngine) Resize(model.Size) { e.mu.Lock(); e.dirty = true; e.mu.Unlock() }
func (e *terminalUITestEngine) Frame(reset bool) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !reset && !e.dirty {
		return nil
	}
	e.dirty = false
	return []byte("\x1b[2J\x1b[Hreign fixture terminal\r\n$ " + e.input)
}
func (e *terminalUITestEngine) Send(b []byte) {
	e.mu.Lock()
	e.input += string(b)
	e.dirty = true
	e.mu.Unlock()
}
func (e *terminalUITestEngine) Done() <-chan struct{} { return e.done }
func (e *terminalUITestEngine) ExitCode() int         { return 0 }
func (e *terminalUITestEngine) Close() error          { e.once.Do(func() { close(e.done) }); return nil }

type terminalUITestProvider struct {
	provider.Provider
	dir string
}

func (p terminalUITestProvider) Transcript(_ context.Context, id, _ string) (provider.Transcript, error) {
	return provider.Transcript{Session: provider.Session{ID: id, CWD: p.dir}}, nil
}
func (p terminalUITestProvider) Status(context.Context) provider.Status {
	return provider.Status{State: "ready"}
}

type terminalUITestAPI struct {
	API
	dispatcher *control.Dispatcher
}

func (a *terminalUITestAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" && strings.HasPrefix(str(r, "method"), "terminal.") {
		b, _ := json.Marshal(r)
		var req control.Request
		_ = json.Unmarshal(b, &req)
		if target != "local" {
			ctx = control.WithRemoteIdentity(ctx, "fixture-controller")
		}
		out := a.dispatcher.HandleRequest(ctx, req)
		if out.Error != "" {
			return nil, &control.ResponseError{Code: out.Code, Message: out.Error}
		}
		var result any
		err := json.Unmarshal(out.Result, &result)
		return result, err
	}
	return a.API.Call(ctx, method, p, target, r)
}

func TestTerminalPanelLifecycleAndShortcut(t *testing.T) {
	engine := &terminalUITestEngine{dirty: true, done: make(chan struct{})}
	nextEngine := &terminalUITestEngine{dirty: true, done: make(chan struct{})}
	var starts atomic.Int32
	manager := model.New(context.Background(), func(context.Context, string, model.Size, string) (model.Engine, error) {
		if starts.Add(1) == 1 {
			return engine, nil
		}
		return nextEngine, nil
	})
	t.Cleanup(func() { _ = manager.Close() })
	d := &control.Dispatcher{Terminals: manager, RemoteTerminalAllowed: func() bool { return true }, Providers: map[string]provider.Provider{"pi": terminalUITestProvider{dir: t.TempDir()}}}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		s.providers = []Object{{"id": "pi", "capabilities": Object{"terminal": true}}}
		s.api = &terminalUITestAPI{s.api, d}
		return s.workspaceView(cx)
	}, nsgotest.Size(1200, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.Type("id:chat-input", "keep this draft")
	editor := app.Find("id:chat-input").ID
	app.Key("ctrl+`")
	app.WaitFor("id:conversation-terminal", 4*time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Find("id:chat-input").Focused })
	app.Key("a")
	app.WaitUntil(time.Second, func() bool { engine.mu.Lock(); defer engine.mu.Unlock(); return engine.input == "a" })
	composer := globalBounds(app.Find("id:composer-card"))
	pane := globalBounds(app.Find("id:terminal-pane"))
	if pane.Origin.Y < composer.Origin.Y+composer.Size.Height || pane.Size.Height < 259 || pane.Size.Height > 261 {
		t.Fatal("terminal not below composer", composer, pane)
	}
	app.Screenshot("terminal-desktop-light")
	app.Compose("label:终端", "ni")
	app.Commit("label:终端", "你")
	app.WaitUntil(time.Second, func() bool { engine.mu.Lock(); defer engine.mu.Unlock(); return engine.input == "a你" })
	app.Key("ctrl+`")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:terminal-pane") })
	if !app.Find("id:chat-input").Focused || app.Find("id:chat-input").ID != editor || app.Find("id:chat-input").Text != "keep this draft" {
		t.Fatal("toggle lost draft/focus/editor")
	}
	select {
	case <-engine.Done():
		t.Fatal("hiding stopped the process")
	default:
	}
	app.Press("id:terminal-toggle")
	app.WaitFor("id:conversation-terminal", 4*time.Second)
	app.Key("b")
	app.WaitUntil(time.Second, func() bool { engine.mu.Lock(); defer engine.mu.Unlock(); return engine.input == "a你b" })
	if starts.Load() != 1 {
		t.Fatal("reopening spawned another process")
	}
	app.Press(sessionSelector("remote", "claude-code", "review"))
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:terminal-pane") })
	select {
	case <-engine.Done():
		t.Fatal("session switch killed shell")
	default:
	}
	app.Press("id:" + sessionRowID("pinned", Object{"deviceId": "local", "providerId": "pi", "id": "design"}))
	app.WaitFor("id:conversation-terminal", 4*time.Second)
	if starts.Load() != 1 || app.Find("id:chat-input").Text != "keep this draft" {
		t.Fatal("switch lost process or draft")
	}
	app.Post(func() { s.setAppearance("dark"); s.changed() })
	app.Settle()
	app.Screenshot("terminal-desktop-dark")
	_ = engine.Close()
	app.WaitFor("text:已退出", 2*time.Second)
	app.Press("id:terminal-reconnect")
	app.WaitUntil(4*time.Second, func() bool { return starts.Load() == 2 && app.Exists("id:conversation-terminal") })
	app.Press("id:terminal-terminate")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:terminal-pane") })
	select {
	case <-nextEngine.Done():
	default:
		t.Fatal("explicit termination did not end shell")
	}
}

// The picker fixture advertises only the shells on its simulated Windows host.
// Delegate accepted opens to the same manager as the default shell.
type terminalProfileFixture struct {
	API
	profiles              []Object
	allowDiscovery        <-chan struct{}
	opened                chan string
	allowOpen, allowClose chan struct{}
}

func (a *terminalProfileFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" {
		switch str(r, "method") {
		case "terminal.profiles":
			if a.allowDiscovery != nil {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-a.allowDiscovery:
				}
			}
			return a.profiles, nil
		case "terminal.openProfile":
			id := str(object(r["params"]), "profile")
			a.opened <- id
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-a.allowOpen:
			}
			if id != "pwsh-7" {
				return nil, errors.New("unexpected fixture profile")
			}
			r = copyObject(r)
			r["method"] = "terminal.open"
			params := copyObject(object(r["params"]))
			delete(params, "profile")
			r["params"] = params
		case "terminal.close":
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-a.allowClose:
			}
		}
	}
	return a.API.Call(ctx, method, p, target, r)
}

func TestTerminalTabsOwnIndependentShells(t *testing.T) {
	first := &terminalUITestEngine{dirty: true, done: make(chan struct{})}
	second := &terminalUITestEngine{dirty: true, done: make(chan struct{})}
	var starts atomic.Int32
	manager := model.New(context.Background(), func(context.Context, string, model.Size, string) (model.Engine, error) {
		switch starts.Add(1) {
		case 1:
			return first, nil
		case 2:
			return second, nil
		default:
			return &terminalUITestEngine{dirty: true, done: make(chan struct{})}, nil
		}
	})
	t.Cleanup(func() { _ = manager.Close() })
	d := &control.Dispatcher{Terminals: manager, Providers: map[string]provider.Provider{"pi": terminalUITestProvider{dir: t.TempDir()}}}
	opened := make(chan string, 2)
	allowOpen, allowClose := make(chan struct{}), make(chan struct{})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := workspaceLayoutFixture(cx)
		s.providers = []Object{{"id": "pi", "capabilities": Object{"terminal": true, "terminalProfiles": true}}}
		s.api = &terminalProfileFixture{API: &terminalUITestAPI{s.api, d}, profiles: []Object{{"id": "cmd", "name": "Command Prompt (cmd)"}, {"id": "pwsh-7", "name": "PowerShell 7"}}, opened: opened, allowOpen: allowOpen, allowClose: allowClose}
		return s.workspaceView(cx)
	}, nsgotest.Size(1050, 800), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))
	waitInput := func(e *terminalUITestEngine, want string) {
		t.Helper()
		app.WaitUntil(2*time.Second, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.input == want })
	}
	app.Press("id:terminal-toggle")
	app.WaitFor("id:conversation-terminal", 3*time.Second)
	app.Key("a")
	waitInput(first, "a")
	app.WaitFor("id:terminal-choose", time.Second)
	app.Press("id:terminal-choose")
	app.WaitFor("id:terminal-profile-pwsh-7", time.Second)
	if app.Exists("id:terminal-profile-pwsh-6") {
		t.Fatal("picker offered an absent shell")
	}
	app.Screenshot("terminal-shell-picker")
	app.Press("id:terminal-profile-pwsh-7")
	app.WaitUntil(time.Second, func() bool { return len(opened) != 0 })
	if <-opened != "pwsh-7" {
		t.Fatal("profile selection did not reach the host")
	}
	// A slow open belongs to the tab even if the user immediately switches.
	app.Press("id:terminal-tab-1")
	app.WaitFor("id:conversation-terminal", 3*time.Second)
	app.Press("id:terminal-tab-2")
	close(allowOpen)
	app.WaitUntil(3*time.Second, func() bool { return starts.Load() == 2 && app.Exists("id:conversation-terminal") })
	app.Key("b")
	waitInput(second, "b")
	app.Press("id:terminal-tab-1")
	app.WaitFor("id:conversation-terminal", 3*time.Second)
	app.Key("c")
	waitInput(first, "ac")
	app.Screenshot("terminal-multiple-tabs")
	app.Press("id:terminal-hide")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:terminal-pane") })
	app.Press("id:terminal-toggle")
	app.WaitFor("id:conversation-terminal", 3*time.Second)
	app.Key("d")
	waitInput(first, "acd")
	app.Press("id:terminal-close-2")
	app.WaitFor("id:terminal-closing-2", time.Second)
	app.Press("id:terminal-hide")
	close(allowClose)
	app.WaitUntil(2*time.Second, func() bool {
		select {
		case <-second.Done():
			return true
		default:
			return false
		}
	})
	app.Press("id:terminal-toggle")
	app.WaitUntil(2*time.Second, func() bool { return !app.Exists("id:terminal-tab-2") })
	select {
	case <-first.Done():
		t.Fatal("closing inactive tab killed the selected shell")
	default:
	}
	if starts.Load() != 2 {
		t.Fatal("switch or collapse created another shell")
	}
	app.WaitFor("id:terminal-choose", time.Second)
	app.Press("id:terminal-add")
	app.WaitUntil(2*time.Second, func() bool { return starts.Load() == 3 && app.Exists("id:terminal-tab-3") })
	if len(opened) != 0 || app.Exists("id:terminal-profile-cmd") {
		t.Fatal("plus selected a profile or opened the picker")
	}
	app.Press("id:terminal-terminate")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:terminal-tab-3") })
	app.Press("id:terminal-terminate")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:terminal-pane") })
}

func TestTerminalDefaultWhileDiscoveringOneShell(t *testing.T) {
	var starts atomic.Int32
	manager := model.New(context.Background(), func(context.Context, string, model.Size, string) (model.Engine, error) {
		starts.Add(1)
		return &terminalUITestEngine{dirty: true, done: make(chan struct{})}, nil
	})
	t.Cleanup(func() { _ = manager.Close() })
	dispatcher := &control.Dispatcher{Terminals: manager, Providers: map[string]provider.Provider{"pi": terminalUITestProvider{dir: t.TempDir()}}}
	allow := make(chan struct{})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := workspaceLayoutFixture(cx)
		s.providers = []Object{{"id": "pi", "capabilities": Object{"terminal": true, "terminalProfiles": true}}}
		s.api = &terminalProfileFixture{API: &terminalUITestAPI{s.api, dispatcher}, profiles: []Object{{"id": "cmd", "name": "cmd"}}, allowDiscovery: allow}
		return s.workspaceView(cx)
	}, nsgotest.Size(950, 700))
	app.Press("id:terminal-toggle")
	app.WaitFor("id:conversation-terminal", 3*time.Second)
	app.Press("id:terminal-add")
	app.WaitUntil(2*time.Second, func() bool { return starts.Load() == 2 && app.Exists("id:conversation-terminal") })
	close(allow)
	app.Settle()
	if app.Exists("id:terminal-choose") {
		t.Fatal("single-shell host exposed a picker")
	}
}
