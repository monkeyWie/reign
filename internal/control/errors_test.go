package control

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type uncertainFixtureError struct{}

func (uncertainFixtureError) Error() string        { return "execution may have completed" }
func (uncertainFixtureError) OutcomeUnknown() bool { return true }

func TestDispatcherPreservesUncertainWriteSemanticsThroughJSON(t *testing.T) {
	d := Dispatcher{Local: func(context.Context, Request) (any, error) { return nil, uncertainFixtureError{} }}
	response := d.Handle(context.Background(), Request{ID: "fixture", Method: "host.fixture"})
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var received Response
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatal(err)
	}
	if !OutcomeUnknown(&ResponseError{Code: received.Code, Message: received.Error}) || received.ID != "fixture" {
		t.Fatal("JSON response lost write outcome semantics", received)
	}
	if OutcomeUnknown(errors.New("definite rejection")) {
		t.Fatal("ordinary failure treated as uncertain")
	}
}
