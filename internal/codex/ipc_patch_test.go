package codex

import (
	"bytes"
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/monkeyWie/reignhub/internal/codexipc"
)

func TestIPCSingleReplacementMatchesGeneralPatch(t *testing.T) {
	doc := json.RawMessage(`{ "turns": [{"items":[{"a/b~c":"old", "":null},42]}], "exact":9007199254740993 }`)
	for _, path := range []string{"/turns/0/items/0/a~1b~0c", "/turns/0/items/0/", "/turns/0/items/1", "/turns/3", "/turns/-1", "/turns/01", "/missing", ""} {
		t.Run(path, func(t *testing.T) {
			patch, err := decodeIPCPatches(rawJSON([]any{map[string]any{"op": "replace", "path": path, "value": "你😀好"}}))
			if err != nil {
				t.Fatal(err)
			}
			before := bytes.Clone(doc)
			got, gotErr := applyIPCPatches(doc, patch)
			want, wantErr := patch.ApplyWithOptions(doc, &jsonpatch.ApplyOptions{AccumulatedCopySizeLimit: codexipc.MaxFrame})
			if (gotErr == nil) != (wantErr == nil) || gotErr == nil && !jsonpatch.Equal(got, want) || !bytes.Equal(before, doc) {
				t.Fatalf("replacement changed transaction semantics: %v / %v", gotErr, wantErr)
			}
			if path == "/turns/0/items/0/a~1b~0c" && !bytes.Contains(got, []byte(`"exact":9007199254740993`)) {
				t.Fatal("unrelated exact number changed")
			}
		})
	}
	if _, _, ok := ipcValueRange([]byte(`{"nested":{"k":1,"k":2}}`), "/nested/k"); ok {
		t.Fatal("ambiguous object used direct replacement")
	}
}
