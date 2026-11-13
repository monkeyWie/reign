package messagecontent

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestQuestionReplies(t *testing.T) {
	data, err := os.ReadFile("testdata/replies.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Plain string
		Parts               []Part
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			parts := Parse(f.Source)
			if len(parts) != 0 || len(f.Parts) != 0 {
				if !reflect.DeepEqual(parts, f.Parts) {
					t.Fatalf("parts = %#v, want %#v", parts, f.Parts)
				}
			}
			if got := PlainText(parts); got != f.Plain {
				t.Fatalf("copy = %q, want %q", got, f.Plain)
			}
		})
	}
}
