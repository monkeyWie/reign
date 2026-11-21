package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAndroidRootBackNeedsTwoPresses(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("checks platform exit requests with the simulated Android host")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, &mobileDemo{newDemoAPI()}, false) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	app.Back()
	app.Find("text:" + rootBackHint)
	if app.BackUnhandled != 0 {
		t.Fatal("first back exited the app")
	}
	app.Advance(rootBackInterval + time.Millisecond)
	if app.Exists("text:" + rootBackHint) {
		t.Fatal("exit hint did not expire")
	}
	app.Back()
	if app.BackUnhandled != 0 {
		t.Fatal("expired confirmation was accepted")
	}
	app.Back()
	if app.BackUnhandled != 1 || app.Exists("text:"+rootBackHint) {
		t.Fatal("second back did not leave the app")
	}
}

func TestAndroidBackConfirmationResetsAfterNavigation(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("checks platform exit requests with the simulated Android host")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, &mobileDemo{newDemoAPI()}, true) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	app.Back()
	app.Find("text:" + rootBackHint)
	openDemoChat(t, app)
	app.Back()
	app.Find("id:chat-input")
	app.Find("text:" + rootBackHint)
	if app.BackUnhandled != 0 {
		t.Fatal("session selection reused the previous exit confirmation")
	}
	showSessionDirectory(t, app)
	app.Press("id:connect-device")
	app.Back()
	app.Find("id:session-sidebar")
	app.Back()
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:mobile-sidebar") })
	app.Find("id:chat-input")
	if app.BackUnhandled != 0 {
		t.Fatal("dismissing overlays exited the app")
	}
	app.Back()
	app.Find("text:" + rootBackHint)
	if app.BackUnhandled != 0 {
		t.Fatal("drawer navigation reused an exit confirmation")
	}
}
