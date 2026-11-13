package main

import (
	"github.com/monkeyWie/reign/clients/nsgo/hubui"
	"github.com/monkeyWie/reign/clients/nsgo/nativecap"
	"github.com/nsgo-dev/nsgo"
)

func main() {
	nsgo.Run(hubui.Root, append([]nsgo.Option{nsgo.WindowSize(1320, 880)}, nativecap.Options()...)...)
}
