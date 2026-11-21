package config

import (
	"encoding/json"
	"github.com/monkeyWie/reignhub/pkg/appdb"
	"strings"
)

func readSettings(dir string, c *Config) error {
	db, e := appdb.Open(appdb.Path(dir))
	if e != nil {
		return e
	}
	defer db.Close()
	rows, e := db.Query("SELECT k,v FROM settings WHERE k GLOB 'config/*'")
	if e != nil {
		return e
	}
	defer rows.Close()
	fields := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var data []byte
		if e = rows.Scan(&key, &data); e != nil {
			return e
		}
		field := strings.TrimPrefix(key, "config/")
		switch {
		case strings.HasPrefix(field, "agents/"):
			var a Agent
			if e = json.Unmarshal(data, &a); e != nil {
				return e
			}
			if c.Agents == nil {
				c.Agents = map[string]Agent{}
			}
			c.Agents[strings.TrimPrefix(field, "agents/")] = a
		case strings.HasPrefix(field, "agentEnabled/"):
			var enabled bool
			if e = json.Unmarshal(data, &enabled); e != nil {
				return e
			}
			if c.AgentEnabled == nil {
				c.AgentEnabled = map[string]bool{}
			}
			c.AgentEnabled[strings.TrimPrefix(field, "agentEnabled/")] = enabled
		default:
			fields[field] = data
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	b, e := json.Marshal(fields)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, c)
}
func writeSettings(c Config) error {
	db, e := appdb.Open(appdb.Path(c.DataDir))
	if e != nil {
		return e
	}
	defer db.Close()
	agents, enabled := c.Agents, c.AgentEnabled
	c.Agents = nil
	c.AgentEnabled = nil
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	values := map[string]json.RawMessage{}
	if e = json.Unmarshal(b, &values); e != nil {
		return e
	}
	for id, v := range agents {
		values["agents/"+id], e = json.Marshal(v)
		if e != nil {
			return e
		}
	}
	for id, v := range enabled {
		values["agentEnabled/"+id], _ = json.Marshal(v)
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	rows, e := tx.Query("SELECT k FROM settings WHERE k GLOB 'config/*'")
	if e != nil {
		return e
	}
	var removed []string
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			rows.Close()
			return e
		}
		if _, ok := values[strings.TrimPrefix(k, "config/")]; !ok {
			removed = append(removed, k)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, k := range removed {
		if _, e = tx.Exec("DELETE FROM settings WHERE k=?", k); e != nil {
			return e
		}
	}
	for k, v := range values {
		if _, e = tx.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v WHERE v!=excluded.v", "config/"+k, []byte(v)); e != nil {
			return e
		}
	}
	return tx.Commit()
}
