// Package service is the public composition API used by both self-hosted ReignHub
// and separately maintained commercial services. It contains no account/billing
// policy; callers configure storage and trusted admission hooks.
package service

import (
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"github.com/monkeyWie/reignhub/internal/webui"
	"gorm.io/gorm"
	"net/http"
)

type Server = deviceservice.Server
type Device = deviceservice.Device
type Admission = deviceservice.Admission

func Open(path string, origins []string) (*Server, error) { return deviceservice.New(path, origins) }
func WithDatabase(db *gorm.DB, origins []string) (*Server, error) {
	return deviceservice.NewWithDB(db, origins)
}
func Web() http.Handler { return webui.Handler() }

var ListenSTUN = connectivity.ListenSTUN
var ListenTCPSTUN = connectivity.ListenTCPSTUN
