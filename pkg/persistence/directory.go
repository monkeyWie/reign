package persistence

import "context"

type DirectoryKey struct{ Env, Provider string }
type DirectoryHead struct {
	Epoch  string
	Cursor int64
}
type DirectoryBatch struct {
	Head           DirectoryHead
	Reset, Preview bool
	Upserts        []Object
	Deletes        []string
	Changes        []DirectoryChange
}
type DirectoryDAO interface {
	DirectoryPosition(context.Context, DirectoryKey) (DirectoryHead, error)
	WriteDirectory(context.Context, DirectoryKey, DirectoryHead, DirectoryBatch) error
	ListSessions(context.Context, DirectoryKey) ([]Object, error)
}

type DirectoryChange struct {
	ID    string
	Value Object
}

// Coalesce the ordered host log before issuing writes. A later recreation
// supersedes an earlier deletion of the same session.
func (b DirectoryBatch) Coalesced() DirectoryBatch {
	if len(b.Changes) == 0 {
		return b
	}
	rows := map[string]Object{}
	for _, v := range b.Upserts {
		id, _ := v["id"].(string)
		rows[id] = v
	}
	for _, id := range b.Deletes {
		rows[id] = nil
	}
	for _, c := range b.Changes {
		rows[c.ID] = c.Value
	}
	b.Upserts = nil
	b.Deletes = nil
	b.Changes = nil
	for id, v := range rows {
		if v == nil {
			b.Deletes = append(b.Deletes, id)
		} else {
			b.Upserts = append(b.Upserts, v)
		}
	}
	return b
}
