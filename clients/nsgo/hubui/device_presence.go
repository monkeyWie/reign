package hubui

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Directory presence is independent of this controller's peer transports.
// One batch per service every 30 seconds, only while the UI is in foreground.
func (s *State) refreshDevicePresence() {
	if s.suspended || s.directoryChecking || len(s.devices) == 0 || time.Since(s.directoryChecked) < 30*time.Second {
		return
	}
	s.directoryChecking = true
	s.directoryChecked = time.Now()
	ctx, cancel := context.WithCancel(s.ctx)
	s.directoryCancel = cancel
	go func() {
		defer cancel()
		v, err := s.api.Call(ctx, "client.presence", nil, "", nil)
		s.cx.Post(func() {
			s.directoryChecking = false
			s.directoryCancel = nil
			if s.ctx.Err() != nil || s.suspended {
				return
			}
			s.devicePresence = map[string]Object{}
			if err == nil {
				for _, status := range objects(v) {
					s.devicePresence[str(status, "id")] = status
				}
			}
			s.changed()
		})
	}()
}

func (s *State) presenceFor(d Object) Object {
	status := s.devicePresence[str(d, "id")]
	if number(status, "checkedAt") < time.Now().Add(-75*time.Second).Unix() {
		return nil
	}
	return status
}

func (s *State) deviceListStatus(d Object) string {
	s.read()
	if flag(d, "connected") {
		return deviceStatus(d)
	}
	if s.connection(str(d, "id")).reconnecting {
		return "正在重新连接…"
	}
	status := s.presenceFor(d)
	if online, ok := status["online"].(bool); ok {
		return deviceStatus(Object{"online": online})
	}
	if s.directoryChecking {
		return "正在获取在线状态…"
	}
	return "在线状态暂不可用"
}

func (s *State) devicePresenceIndicator(cx *core.Scope, d func() Object) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		s.read()
		device := d()
		if flag(device, "connected") || s.connection(str(device, "id")).reconnecting {
			return s.connectionIndicator(cx, func() string { return str(d(), "id") })
		}
		color := palette(cx).mutedText.Faded(.5)
		if online, known := s.presenceFor(device)["online"].(bool); known && online {
			color = core.RGB(0x22a060)
		}
		return core.View{TestID: "device-presence-" + str(device, "id"), Label: s.deviceListStatus(device), Style: core.Style{Width: core.Pt(18), Height: core.Pt(18), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}, Children: []core.Node{core.View{Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: 4, Background: color}}}}
	})
}

// Navigation is local. Connection setup runs behind the status indicator and
// must never gate reading cached sessions or messages.
func (s *State) openDevice(id string) {
	for _, d := range s.devices {
		if str(d, "id") != id {
			continue
		}
		s.selectDevice(id)
		if flag(d, "connected") || s.connection(id).reconnecting {
			return
		}
		s.connection(id).established = false
		s.connectDevice(id, false, !flag(d, "autoReconnect"))
		return
	}
}
