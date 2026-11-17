package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// Codex broadcasts Immer paths as arrays of object keys and array indices.
// Normalize those paths to RFC 6901 before applying the whole transaction.
// acceptedTextChanges is an optional projection hint: patches carry the state.
func decodeIPCPatches(raw json.RawMessage) (jsonpatch.Patch, error) {
	var patches []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &patches); err != nil {
		return nil, err
	}
	for _, patch := range patches {
		for _, field := range []string{"path", "from"} {
			value, exists := patch[field]
			if !exists {
				continue
			}
			var pointer string
			if len(value) > 0 && value[0] == '"' && json.Unmarshal(value, &pointer) == nil {
				continue
			}
			var path []any
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.UseNumber()
			if err := decoder.Decode(&path); err != nil || path == nil {
				return nil, fmt.Errorf("IPC patch 路径无效: %s", field)
			}
			segments := make([]string, len(path))
			for i, part := range path {
				switch part := part.(type) {
				case string:
					segments[i] = jsonPointer(part)
				case json.Number:
					index, err := part.Int64()
					if err != nil || index < 0 {
						return nil, fmt.Errorf("IPC patch 数组索引无效")
					}
					segments[i] = part.String()
				default:
					return nil, fmt.Errorf("IPC patch 路径元素无效")
				}
			}
			if len(path) > 0 {
				pointer = "/" + strings.Join(segments, "/")
			}
			patch[field] = rawJSON(pointer)
		}
	}
	return jsonpatch.DecodePatch(rawJSON(patches))
}
