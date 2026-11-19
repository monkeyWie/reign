package hubui

// A native editor can report its submitted value after the send handler has
// consumed it. Do not resurrect that value without a new editing action. Keep
// the exact text (including whitespace) and route; never suppress a new draft.
func (s *State) changeDraft(text string) {
	if !s.draftReady() {
		return
	}
	if s.submittedDraftSession == s.key() && s.submittedDraft != "" && text == s.submittedDraft && s.draft.GetUntracked() == "" {
		return
	}
	if text != "" {
		s.beginDraftEdit()
	}
	s.draft.Set(text)
	s.persistDraft()
}

func (s *State) beginDraftEdit() {
	s.submittedDraft, s.submittedDraftSession = "", ""
}

func (s *State) submitDraft(text string, method string) {
	if !s.canSubmitMessage() {
		return
	}
	// Return carries the editor's final value, including an input-method commit
	// that may be newer than the controlled signal. Use the same echo guard.
	s.changeDraft(text)
	s.send(method)
}
