// Package brandicons shares platform, Agent and browser artwork with the website.
package brandicons

import (
	_ "embed"
	"encoding/json"
)

//go:embed paths.json
var pathsJSON []byte

func Paths() map[string]string {
	var paths map[string]string
	if err := json.Unmarshal(pathsJSON, &paths); err != nil {
		panic(err)
	}
	return paths
}
