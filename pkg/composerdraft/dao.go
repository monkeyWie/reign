package composerdraft

import (
	"context"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

type DAO interface {
	ReadDraft(context.Context, persistence.Session) (Draft, error)
	SaveDraft(context.Context, persistence.Session, Draft) error
}

func Dispatch(ctx context.Context, dao DAO, method, target string, p map[string]any) (any, error) {
	provider, _ := p["provider"].(string)
	session, _ := p["session"].(string)
	k := persistence.Session{Env: target, Provider: provider, ID: session}
	if !k.Valid() {
		return nil, fmt.Errorf("invalid draft identity")
	}
	if method == "client.draft.get" {
		return dao.ReadDraft(ctx, k)
	}
	if method != "client.draft.put" {
		return nil, fmt.Errorf("unknown draft operation")
	}
	d, ok := p["draft"].(Draft)
	if !ok {
		if e := decode(p["draft"], &d); e != nil {
			return nil, e
		}
	}
	if len(d.Files) > 16 || len(d.Text) > 4<<20 {
		return nil, fmt.Errorf("draft exceeds storage limit")
	}
	total := 0
	for _, f := range d.Files {
		if f.Size < 0 || f.Size > 50<<20 || len(f.Hash) != 64 {
			return nil, fmt.Errorf("invalid draft attachment")
		}
		total += f.Size
		if total > 100<<20 {
			return nil, fmt.Errorf("draft exceeds storage limit")
		}
		if (f.Data != nil || f.Size == 0) && (len(f.Data) != f.Size || Hash(f.Data) != f.Hash) {
			return nil, fmt.Errorf("invalid attachment content")
		}
	}
	return nil, dao.SaveDraft(ctx, k, d)
}
