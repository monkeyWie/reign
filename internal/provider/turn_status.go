package provider

import "context"

// TurnStatusProvider avoids normalizing messages and importing attachments when
// a background notification only needs the state of a known task.
type TurnStatusProvider interface {
	TurnStatus(context.Context, string, string) (Turn, error)
}
