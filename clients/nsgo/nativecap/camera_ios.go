//go:build ios

package nativecap

/*
#cgo LDFLAGS: -framework UIKit -framework AVFoundation -framework Foundation -framework CoreGraphics
#include "camera_ios.h"
*/
import "C"

import (
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"sync"
	"sync/atomic"
	"unsafe"
)

var cameraID atomic.Uint64
var cameras sync.Map

type cameraView struct {
	view  uintptr
	token uint64
}

func platformOptions() []nsgo.Option {
	host := core.NewHost()
	host.Handle("ras.timezone", func(call *core.HostCall) {
		p := C.ras_timezone_name()
		defer C.free(unsafe.Pointer(p))
		call.Resolve(map[string]any{"name": C.GoString(p), "offsetSeconds": int(C.ras_timezone_offset())})
	})
	host.Handle("ras.device", func(call *core.HostCall) {
		p := C.ras_device_name()
		defer C.free(unsafe.Pointer(p))
		call.Resolve(map[string]any{"name": C.GoString(p)})
	})
	return []nsgo.Option{nsgo.HostChannel(host), nsgo.Component("ras.qr-camera", func(cx *core.NativeContext) core.NativeComponent {
		id := cameraID.Add(1)
		cameras.Store(id, cx)
		return &cameraView{token: id, view: uintptr(C.ras_camera_create(C.uint64_t(id)))}
	})}
}
func (c *cameraView) View() any { return c.view }
func (c *cameraView) SetProp(name string, value any) {
	if name == "active" {
		n := 0
		if value != false {
			n = 1
		}
		C.ras_camera_active(C.uintptr_t(c.view), C.int(n))
	}
}
func (c *cameraView) Command(name string, _ any) {
	if name == "retry" {
		C.ras_camera_retry(C.uintptr_t(c.view))
	}
}
func (c *cameraView) Close() { cameras.Delete(c.token); C.ras_camera_close(C.uintptr_t(c.view)) }

//export rasQrEvent
func rasQrEvent(token C.uint64_t, name, value *C.char) {
	if cx, ok := cameras.Load(uint64(token)); ok {
		cx.(*core.NativeContext).Emit(C.GoString(name), C.GoString(value))
	}
}
