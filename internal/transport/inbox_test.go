package transport

import "testing"

func TestSequencedFragmentsSurvivePathChange(t *testing.T) {
	in := NewInbox[string]()
	in.PushSequence("final", 5, 3)
	in.PushSequence("second", 6, 2)
	in.PushSequence("second", 6, 2)
	if _, ok, err := in.Take(); ok || err != nil {
		t.Fatal("published incomplete snapshot")
	}
	in.PushSequence("first", 5, 1)
	for _, want := range []string{"first", "second", "final"} {
		got, ok, err := in.Take()
		if !ok || err != nil || got != want {
			t.Fatal(got, ok, err)
		}
	}
}
