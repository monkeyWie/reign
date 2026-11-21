package codex

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/monkeyWie/reignhub/internal/codexipc"
)

// Owners stream text and metadata as replacements. An existing value has a
// contiguous range in the immutable JSON revision. Share the parser's input
// buffer and copy the surrounding bytes once, preserving unrelated numbers,
// escaping and unknown fields verbatim. Other transactions use RFC 6902.
func applyIPCPatches(doc json.RawMessage, patch jsonpatch.Patch) ([]byte, error) {
	if len(patch) == 1 && patch[0].Kind() == "replace" {
		path, err := patch[0].Path()
		value := patch[0]["value"]
		pointer := jsontext.Pointer(path)
		// The known streaming fields are at most eight levels deep.
		if err == nil && path != "" && strings.Count(path, "/") <= 8 && pointer.IsValid() && value != nil {
			start, end, ok := ipcValueRange(doc, pointer)
			if ok && len(doc)-(end-start)+len(*value) <= codexipc.MaxFrame {
				next := make([]byte, 0, len(doc)-(end-start)+len(*value))
				next = append(next, doc[:start]...)
				next = append(next, (*value)...)
				return append(next, doc[end:]...), nil
			}
		}
	}
	return patch.ApplyWithOptions(doc, &jsonpatch.ApplyOptions{SupportNegativeIndices: false, AccumulatedCopySizeLimit: codexipc.MaxFrame})
}

// Read the whole containing object at each level so duplicate names and
// non-canonical array indices always fall back to the general patch engine.
func ipcValueRange(doc []byte, path jsontext.Pointer) (int, int, bool) {
	start, end := 0, len(doc)
	for name := range path.Tokens() {
		dec := jsontext.NewDecoder(bytes.NewBuffer(doc[start:end]))
		kind, err := dec.ReadToken()
		if err != nil || kind.Kind() != '{' && kind.Kind() != '[' {
			return 0, 0, false
		}
		array := kind.Kind() == '['
		index := -1
		if array {
			index, err = strconv.Atoi(name)
			if err != nil || index < 0 || strconv.Itoa(index) != name {
				return 0, 0, false
			}
		}
		lo, hi := -1, -1
		for i := 0; dec.PeekKind() != '}' && dec.PeekKind() != ']'; i++ {
			match := i == index
			if !array {
				key, err := dec.ReadToken()
				if err != nil {
					return 0, 0, false
				}
				match = key.String() == name
			}
			if match {
				value, err := dec.ReadValue()
				if err != nil {
					return 0, 0, false
				}
				hi = int(dec.InputOffset())
				lo = hi - len(value)
			} else if err := dec.SkipValue(); err != nil {
				return 0, 0, false
			}
		}
		if lo < 0 {
			return 0, 0, false
		}
		start, end = start+lo, start+hi
	}
	return start, end, true
}
