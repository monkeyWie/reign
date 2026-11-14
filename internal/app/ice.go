package app

import (
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/remote"
	"github.com/pion/webrtc/v4"
)

func ICE(c config.Config) remote.Options {
	servers := []webrtc.ICEServer{}
	if len(c.STUN) > 0 {
		servers = append(servers, webrtc.ICEServer{URLs: c.STUN})
	}
	return remote.Options{ICEServers: connectivity.STUNOnly(servers)}
}
