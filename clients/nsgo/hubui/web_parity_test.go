package hubui

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Both UIs consume the same fixtures so presentation changes stay aligned.
func TestWebNativePresentationParity(t *testing.T) {
	data, err := os.ReadFile("../../../packages/protocol/testdata/chat-presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name       string `json:"name"`
		Transcript Object `json:"transcript"`
		IDs, Roles []string
		Live       []bool
		Thinking   bool
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			rows := messages(f.Transcript)
			ids, roles, live := []string{}, []string{}, []bool{}
			for _, row := range rows {
				ids = append(ids, row.ID)
				roles = append(roles, row.Role)
				live = append(live, row.Live)
			}
			if !reflect.DeepEqual(ids, f.IDs) || !reflect.DeepEqual(roles, f.Roles) || !reflect.DeepEqual(live, f.Live) {
				t.Fatalf("unexpected rows: %+v", rows)
			}
			active := activeTurn(f.Transcript)
			thinking := active != nil
			for _, item := range objects(active["items"]) {
				if visibleTool(item) || str(item, "role") == "assistant" && strings.TrimSpace(str(item, "text")) != "" {
					thinking = false
				}
			}
			if thinking != f.Thinking {
				t.Fatalf("thinking=%v want %v", thinking, f.Thinking)
			}
		})
	}
}
