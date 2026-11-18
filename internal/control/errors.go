package control

import "errors"

const WriteOutcomeUnknown = "WRITE_OUTCOME_UNKNOWN"

// ResponseError preserves machine-readable failure semantics across JSON APIs.
type ResponseError struct {
	Code    string
	Message string
}

func (e *ResponseError) Error() string        { return e.Message }
func (e *ResponseError) OutcomeUnknown() bool { return e.Code == WriteOutcomeUnknown }

func OutcomeUnknown(err error) bool {
	var outcome interface{ OutcomeUnknown() bool }
	return errors.As(err, &outcome) && outcome.OutcomeUnknown()
}

func ErrorCode(err error) string {
	if OutcomeUnknown(err) {
		return WriteOutcomeUnknown
	}
	var response *ResponseError
	if errors.As(err, &response) {
		return response.Code
	}
	return ""
}
