//go:build js && wasm

package hubui

import (
	"testing"

	"github.com/monkeyWie/reignhub/clients/nsgo/webapi"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func timelineTestDAO(t *testing.T) persistence.HistoryDAO { return &webapi.Store{} }
