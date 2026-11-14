package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"github.com/monkeyWie/reignhub/internal/bridge"
	"unsafe"
)

//export AgentHubCall
func AgentHubCall(input *C.char) *C.char { return C.CString(bridge.Call(C.GoString(input))) }

//export AgentHubFree
func AgentHubFree(p *C.char) { C.free(unsafe.Pointer(p)) }

//export AgentHubShutdown
func AgentHubShutdown() { bridge.Shutdown() }

func main() {}
