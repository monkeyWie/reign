//go:build ios || android || js || (!darwin && !windows && !linux)

package hubui

import "github.com/nsgo-dev/nsgo/core"

func initializeTerminalHost()                                         {}
func (s *State) terminalButton(*core.Scope) core.Node                 { return nil }
func (s *State) terminalSettings(*core.Scope) core.Node               { return nil }
func (s *State) withTerminal(_ *core.Scope, chat core.Node) core.Node { return chat }
