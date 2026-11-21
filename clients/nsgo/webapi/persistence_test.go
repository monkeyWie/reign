package webapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/monkeyWie/reignhub/pkg/composerdraft"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"sort"
	"strings"
	"time"
)

func (s *memoryStore) ApplyHistory(ctx context.Context, k persistence.Session, p persistence.HistoryPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !p.Replace {
		return errors.New("fixture expects an initial snapshot")
	}
	if s.histories == nil {
		s.histories = map[persistence.Session]Object{}
	}
	s.histories[k] = persistence.Assemble(p.Head, p.Turns, p.Messages)
	return nil
}
func (s *memoryStore) ReadPage(ctx context.Context, k persistence.Session, q persistence.PageQuery) (persistence.HistoryPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return persistence.HistoryPage{Transcript: s.histories[k]}, nil
}
func (s *memoryStore) ForgetHistory(ctx context.Context, env string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.v, "env/"+env)
	for k := range s.histories {
		if k.Env == env {
			delete(s.histories, k)
		}
	}
	for k := range s.dirs {
		if k.Env == env {
			delete(s.dirs, k)
			delete(s.heads, k)
		}
	}
	return nil
}
func (s *memoryStore) DirectoryPosition(ctx context.Context, k persistence.DirectoryKey) (persistence.DirectoryHead, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heads[k], nil
}
func (s *memoryStore) WriteDirectory(ctx context.Context, k persistence.DirectoryKey, h persistence.DirectoryHead, b persistence.DirectoryBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirs == nil {
		s.dirs = map[persistence.DirectoryKey]map[string]Object{}
		s.heads = map[persistence.DirectoryKey]persistence.DirectoryHead{}
	}
	if _, ok := s.heads[k]; b.Preview && ok {
		return nil
	}
	if !b.Preview {
		s.heads[k] = b.Head
	}
	if b.Reset || s.dirs[k] == nil {
		s.dirs[k] = map[string]Object{}
	}
	for _, v := range b.Upserts {
		s.dirs[k][str(v, "id")] = v
	}
	for _, id := range b.Deletes {
		delete(s.dirs[k], id)
	}
	return nil
}
func (s *memoryStore) ListSessions(ctx context.Context, k persistence.DirectoryKey) ([]Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Object
	for scope, dir := range s.dirs {
		if scope.Env == k.Env && (k.Provider == "" || scope.Provider == k.Provider) {
			for _, v := range dir {
				out = append(out, v)
			}
		}
	}
	return out, nil
}
func (s *memoryStore) SaveList(ctx context.Context, k persistence.Session, kind persistence.ReadKind, list []Object) error {
	return s.Put("list/"+k.Env+"/"+k.Provider+"/"+k.ID+"/"+string(kind), list)
}
func (s *memoryStore) LoadList(ctx context.Context, k persistence.Session, kind persistence.ReadKind) ([]Object, error) {
	v, e := s.Get("list/" + k.Env + "/" + k.Provider + "/" + k.ID + "/" + string(kind))
	if v == nil {
		return nil, e
	}
	return arr(v), e
}

func (s *memoryStore) Route(ctx context.Context, peer string) (string, error) { return peer, nil }
func (s *memoryStore) Environments() ([]Object, error) {
	keys, err := s.Keys()
	sort.Strings(keys)
	if err != nil {
		return nil, err
	}
	var out []Object
	for _, k := range keys {
		if strings.HasPrefix(k, "env/") {
			v, e := s.Get(k)
			if e != nil {
				return nil, e
			}
			out = append(out, obj(v))
		}
	}
	return out, nil
}
func (s *memoryStore) SaveEnvironment(ctx context.Context, v Object) error {
	return s.Put("env/"+str(v, "id"), v)
}

func (s *memoryStore) Download(ctx context.Context, method, target string, p Object) (any, error) {

	b, _ := json.Marshal([]string{target, str(p, "key")})
	key := "asset:" + string(b)
	v, e := s.Get("asset-index")
	if e != nil {
		return nil, e
	}
	index := arr(v)
	if method == "client.assetCache.get" {
		v, e := s.Get(key)
		if e == nil && v != nil {
			for _, x := range index {
				if x["key"] == key {
					x["touched"] = time.Now().UnixMilli()
				}
			}
			_ = s.Put("asset-index", index)
		}
		return v, e
	}
	if method == "client.assetCache.clear" {
		out := []Object{}
		for _, x := range index {
			if target == "" || x["device"] == target {
				_ = s.Delete(str(x, "key"))
			} else {
				out = append(out, x)
			}
		}
		return nil, s.Put("asset-index", out)
	}
	bytes, e := base64.StdEncoding.DecodeString(str(p, "data"))
	if e != nil || len(bytes) > 50<<20 {
		return nil, errors.New("附件超过缓存上限或格式无效")
	}
	out := []Object{}
	var size int64
	for _, x := range index {
		if x["key"] != key {
			out = append(out, x)
			size += num(x, "size")
		}
	}
	for size+int64(len(bytes)) > 96<<20 && len(out) > 0 {
		old := 0
		for i, x := range out {
			if num(x, "touched") < num(out[old], "touched") {
				old = i
			}
		}
		_ = s.Delete(str(out[old], "key"))
		size -= num(out[old], "size")
		out = append(out[:old], out[old+1:]...)
	}
	if e = s.Put(key, Object{"data": p["data"]}); e != nil {
		return nil, e
	}
	return nil, s.Put("asset-index", append(out, Object{"key": key, "device": target, "size": len(bytes), "touched": time.Now().UnixMilli()}))
}

func (s *memoryStore) LoadIdentity(ctx context.Context, service string) (Object, error) {
	v, e := s.Get("identity:" + service)
	if v == nil {
		return nil, e
	}
	return obj(v), e
}
func (s *memoryStore) SaveIdentity(ctx context.Context, service string, v Object) error {
	return s.Put("identity:"+service, v)
}
func (s *memoryStore) SaveDraft(ctx context.Context, k persistence.Session, d composerdraft.Draft) error {
	_, e := composerdraft.Call(ctx, s, "client.draft.put", k.Env, Object{"provider": k.Provider, "session": k.ID, "draft": d})
	return e
}
func (s *memoryStore) ReadDraft(ctx context.Context, k persistence.Session) (composerdraft.Draft, error) {
	v, e := composerdraft.Call(ctx, s, "client.draft.get", k.Env, Object{"provider": k.Provider, "session": k.ID})
	if e != nil {
		return composerdraft.Draft{}, e
	}
	return v.(composerdraft.Draft), nil
}
