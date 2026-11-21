package hubui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type lazyCatalogueAPI struct {
	*demoAPI
	release                    chan struct{}
	syncs, transcripts, models atomic.Int32
	unrelatedTranscript        atomic.Bool
}

type refreshCatalogueAPI struct {
	*lazyCatalogueAPI
	fail bool
}

func (f *refreshCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	v, err := f.lazyCatalogueAPI.Call(ctx, method, p, target, r)
	if err == nil && f.fail && method == "client.sync" && str(p, "provider") == "codex-app" {
		return nil, errors.New("fixture catalogue unavailable")
	}
	return v, err
}

func TestSessionRefreshFeedback(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			f := &refreshCatalogueAPI{lazyCatalogueAPI: &lazyCatalogueAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}, fail: outcome == "failure"}
			var s *State
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetTheme(core.DefaultTheme())
				s = newState(cx, f)
				s.devices = []Object{{"id": "demo-device", "connected": true}}
				s.target = "demo-device"
				s.sessions = []Object{{"id": "demo-chat", "providerId": "codex-app", "title": "Cached conversation", "cwd": "/workspace/agenthub"}}
				return s.directory(cx)
			}, nsgotest.Size(390, 844))
			ensureSidebarMenu(app)
			app.Press("id:refresh-sessions")
			app.WaitFor("id:refresh-sessions-loading", time.Second)
			app.WaitUntil(time.Second, func() bool { return f.syncs.Load() == 1 })
			ensureSidebarMenu(app)
			control := app.Find("id:refresh-sessions")
			if !control.Props.Busy || !control.Props.Disabled || control.Props.Label == "" {
				t.Fatal("refresh must acknowledge pending work and prevent duplicate activation")
			}
			spinner := app.Find("id:refresh-sessions-loading").ID
			for range 3 {
				app.PressAt("id:refresh-sessions", core.Point{X: 12, Y: 12})
				app.Post(func() { s.cx.Post(s.changed) })
				app.Settle()
				if app.Find("id:refresh-sessions-loading").ID != spinner {
					t.Fatal("unrelated updates restarted refresh animation")
				}
			}
			if f.syncs.Load() != 1 {
				t.Fatal("duplicate refresh request")
			}
			app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
			app.Screenshot("session-refresh-" + outcome)
			if outcome == "cancel" {
				app.Post(func() { s.cx.Post(func() { s.cancelCatalogue(); s.changed() }) })
			} else {
				close(f.release)
			}
			app.WaitUntil(time.Second, func() bool { return !app.Exists("id:refresh-sessions-loading") })
			control = app.Find("id:refresh-sessions")
			if control.Props.Busy || control.Props.Disabled {
				t.Fatal("refresh feedback did not finish")
			}
			app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
			if outcome == "failure" {
				app.Post(func() {
					s.cx.Post(func() {
						if !strings.Contains(s.connectionDetails("demo-device"), "codex-app: fixture catalogue unavailable") {
							t.Error("ready provider failure lost its provider scope")
						}
					})
				})
				app.Settle()
			}
		})
	}
}

type catalogueReply struct {
	ctx    context.Context
	result chan []Object
}

type orderedCatalogueAPI struct {
	*demoAPI
	requests chan catalogueReply
}

func (*orderedCatalogueAPI) CanHost() bool { return false }
func (f *orderedCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.sync" && str(p, "provider") == "codex-app" {
		request := catalogueReply{ctx, make(chan []Object, 1)}
		f.requests <- request
		// Model a response already arriving when cancellation races completion.
		return <-request.result, nil
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestSessionRefreshBackgroundIsSilentAndManualSupersedesIt(t *testing.T) {
	f := &orderedCatalogueAPI{newDemoAPI(), make(chan catalogueReply, 4)}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, f)
		s.devices = []Object{{"id": "demo-device", "connected": true}}
		s.target = "demo-device"
		s.sessions = []Object{{"id": "cached", "providerId": "codex-app", "title": "Cached conversation", "cwd": "/workspace/agenthub"}}
		return s.directory(cx)
	}, nsgotest.Size(390, 844))
	next := func() catalogueReply {
		t.Helper()
		app.WaitUntil(2*time.Second, func() bool { return len(f.requests) > 0 })
		select {
		case r := <-f.requests:
			t.Cleanup(func() {
				select {
				case r.result <- nil:
				default:
				}
			})
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("catalogue request never started")
			return catalogueReply{}
		}
	}
	app.Post(func() { s.cx.Post(s.poll) })
	background := next()
	app.Settle()
	ensureSidebarMenu(app)
	control := app.Find("id:refresh-sessions")
	if control.Props.Busy || control.Props.Disabled || app.Exists("id:refresh-sessions-loading") {
		t.Fatal("background refresh animated or disabled the manual refresh control")
	}
	app.WaitFor(sessionSelector("demo-device", "codex-app", "cached"), time.Second)
	app.Screenshot("background-refresh-silent")
	app.PressAt("id:refresh-sessions", core.Point{X: 12, Y: 12})
	manual := next()
	app.WaitFor("id:refresh-sessions-loading", time.Second)
	select {
	case <-background.ctx.Done():
	default:
		t.Fatal("manual refresh did not cancel the older read")
	}
	background.result <- []Object{{"id": "stale", "providerId": "codex-app", "title": "Stale conversation"}}
	app.Advance(100 * time.Millisecond)
	if !app.Exists("id:refresh-sessions-loading") || app.Exists(sessionSelector("demo-device", "codex-app", "stale")) {
		t.Fatal("stale background response affected the manual request")
	}
	manual.result <- []Object{{"id": "fresh", "providerId": "codex-app", "title": "Fresh conversation"}}
	app.WaitFor(sessionSelector("demo-device", "codex-app", "fresh"), time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:refresh-sessions-loading") })
	if app.Exists(sessionSelector("demo-device", "codex-app", "stale")) || app.Find("id:refresh-sessions").Props.Disabled {
		t.Fatal("manual refresh did not finish with the fresh result")
	}
}

func (*lazyCatalogueAPI) CanHost() bool { return false }
func (f *lazyCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.cached" {
		return nil, nil
	}
	if method == "client.sync" && str(p, "provider") == "codex-app" {
		f.syncs.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
		list, err := f.demoAPI.Call(ctx, method, p, target, r)
		return append(objects(list), Object{"id": "older-chat", "providerId": "codex-app", "title": "更早的会话", "cwd": "/workspace/agenthub"}), err
	}
	if method == "client.call" {
		switch str(r, "method") {
		case "transcript":
			f.transcripts.Add(1)
			if str(r, "session") != "demo-chat" {
				f.unrelatedTranscript.Store(true)
			}
		case "models":
			f.models.Add(1)
		case "watch":
			if flag(r, "probe") {
				return Object{"epoch": "catalogue", "cursor": number(r, "after"), "reset": str(r, "epoch") != "catalogue"}, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func TestColdCatalogueIsUsableBeforeFullScanAndMessagesLoadOnOpen(t *testing.T) {
	f := &lazyCatalogueAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844))
	showSessionDirectory(t, app)
	app.WaitFor("id:device-demo-device", time.Second)
	app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
	// A different provider must remain usable while the App directory is blocked.
	app.WaitFor(sessionSelector("demo-device", "pi", "demo-pi"), time.Second)
	app.WaitUntil(time.Second, func() bool { return f.syncs.Load() > 0 })
	if f.transcripts.Load() != 0 || f.models.Load() != 0 {
		t.Fatal("opening a device eagerly loaded messages/models")
	}
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(time.Second, func() bool { return f.transcripts.Load() > 0 })
	app.WaitFor("id:message-demo-turn|demo-assistant", time.Second)
	close(f.release)
	app.Press("id:mobile-menu")
	app.WaitFor(sessionSelector("demo-device", "codex-app", "older-chat"), time.Second)
	// The independent history read may finish before the live join and need
	// one reconciliation. Check which session was read, not that scheduling.
	if f.unrelatedTranscript.Load() {
		t.Fatal("directory completion loaded unrelated transcripts")
	}
}

type disappearingProviderAPI struct {
	*independentStreamAPI
	missing, failSync atomic.Bool
}

func (f *disappearingProviderAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" && str(r, "method") == "providers" && f.missing.Load() {
		return []Object{}, nil
	}
	if method == "client.sync" && f.failSync.Load() {
		return nil, errors.New("directory temporarily unavailable")
	}
	return f.independentStreamAPI.Call(ctx, method, p, target, r)
}
func TestBackFromStalledHistoryPreservesCatalogueAcrossDiscoveryFailure(t *testing.T) {
	f := &disappearingProviderAPI{independentStreamAPI: &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844))
	showSessionDirectory(t, app)
	app.WaitFor("id:device-demo-device", time.Second)
	app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	app.WaitFor("id:message-demo-turn|demo-assistant", time.Second)
	f.missing.Store(true)
	app.Press("id:mobile-menu")
	app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
	ensureSidebarMenu(app)
	app.Press("id:refresh-sessions")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:refresh-sessions-loading") })
	app.Find(sessionSelector("demo-device", "codex-app", "demo-chat"))
	f.missing.Store(false)
	f.failSync.Store(true)
	ensureSidebarMenu(app)
	app.Press("id:refresh-sessions")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:refresh-sessions-loading") })
	app.Find(sessionSelector("demo-device", "codex-app", "demo-chat"))
}

type unavailableCatalogueAPI struct {
	*demoAPI
	ready             atomic.Bool
	piReads, appSyncs atomic.Int32
}

func (f *unavailableCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" && str(r, "method") == "providers" {
		providers := demoProviders()
		if !f.ready.Load() {
			providers[1]["state"] = "unavailable"
			providers[1]["detail"] = "Pi is not ready"
		}
		return providers, nil
	}
	if method == "client.sync" && str(p, "provider") == "codex-app" {
		f.appSyncs.Add(1)
	}
	if method == "client.sync" && str(p, "provider") == "pi" || method == "client.call" && str(r, "method") == "sessions" && str(r, "provider") == "pi" {
		f.piReads.Add(1)
		if !f.ready.Load() {
			return nil, errors.New("Agent is not ready")
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestCatalogueSkipsUnavailableHistoryProviderAndResumesWhenReady(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "cold"
		if cached {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			f := &unavailableCatalogueAPI{demoAPI: newDemoAPI()}
			var s *State
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetTheme(core.DefaultTheme())
				s = newState(cx, f)
				s.target = "demo-device"
				s.devices = []Object{{"id": s.target, "connected": true}}
				if cached {
					s.sessions = []Object{{"id": "demo-pi", "providerId": "pi", "title": "Cached Pi conversation"}}
				}
				return s.directory(cx)
			}, nsgotest.Size(390, 844))
			refresh := func(want int32) {
				ensureSidebarMenu(app)
				app.Press("id:refresh-sessions")
				app.WaitUntil(time.Second, func() bool {
					return f.appSyncs.Load() >= want && !app.Exists("id:refresh-sessions-loading")
				})
				app.Post(func() {
					s.cx.Post(func() {
						if detail := s.connectionDetails("demo-device"); detail != "" {
							t.Error("unavailable provider created a connection issue", detail)
						}
					})
				})
				app.Settle()
			}
			refresh(1)
			app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
			if f.piReads.Load() != 0 {
				t.Fatal("unavailable history provider received directory requests")
			}
			if cached {
				app.WaitFor(sessionSelector("demo-device", "pi", "demo-pi"), time.Second)
			}
			f.ready.Store(true)
			refresh(2)
			app.WaitFor(sessionSelector("demo-device", "pi", "demo-pi"), time.Second)
			if f.piReads.Load() == 0 {
				t.Fatal("provider did not resume synchronization after becoming ready")
			}
			reads := f.piReads.Load()
			f.ready.Store(false)
			refresh(3)
			app.WaitFor(sessionSelector("demo-device", "pi", "demo-pi"), time.Second)
			if f.piReads.Load() != reads {
				t.Fatal("provider becoming unavailable restarted rejected reads")
			}
		})
	}
}

func TestSessionTitlesSurvivePartialMetadataAndAcceptRenames(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 820))
	row := copyObject(s.selected)
	key := "id:" + sessionRowID("pinned", row)
	title := str(row, "title")
	apply := func(name string, directory bool) {
		t.Helper()
		app.Post(func() {
			s.cx.Post(func() {
				next := copyObject(row)
				next["title"] = name
				if directory {
					s.applyCataloguePage(str(row, "providerId"), []Object{next}, true)
				} else {
					s.setTranscript(Object{"session": next, "turns": s.transcript["turns"]})
				}
			})
		})
		app.Settle()
	}
	assertTitle := func(want string) {
		t.Helper()
		if actual := app.Find(key).Props.Label; !strings.HasPrefix(actual, want+" ·") {
			t.Fatalf("directory title = %q, want %q", actual, want)
		}
	}
	apply("", false)
	assertTitle(title)
	apply(" ", true)
	assertTitle(title)
	apply("Renamed fixture", true)
	assertTitle("Renamed fixture")
	apply(title, false)
	assertTitle("Renamed fixture")
}

func TestIdleRemoteDirectoryBacksOffAndManualRefreshRunsImmediately(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, newDemoAPI())
		s.devices = []Object{{"id": "demo-device", "connected": true}}
		s.target = "demo-device"
		return core.View{}
	})
	c := s.catalogue("demo-device")
	for _, want := range []time.Duration{2, 4, 8, 16, 30, 30} {
		app.Post(func() { c.nextPoll = time.Time{}; s.pollCatalogues() })
		app.WaitUntil(time.Second, func() bool { return !c.polling && c.pollInterval == want*time.Second })
	}
	stamp := c.generation
	app.Post(s.pollCatalogues)
	app.Settle()
	if c.generation != stamp {
		t.Fatal("idle timer started an unnecessary directory scan")
	}
	app.Post(s.refreshSessions)
	app.WaitUntil(time.Second, func() bool { return c.generation > stamp && !c.polling && c.pollInterval == 2*time.Second })
}
