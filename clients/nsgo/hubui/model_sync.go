package hubui

// modelBase is the last known Agent model. Repeated metadata must not undo an
// unsent local choice, but an actual external change supersedes that choice.
func (s *State) syncModel(model string) {
	if model == "" || model == s.modelBase {
		return
	}
	if s.model == "" || s.modelBase != "" {
		s.model = model
	}
	s.modelBase = model
	s.persistDraft()
}

func (s *State) restoreDraftModel(d draftState) {
	if d.Model == "" {
		return
	}
	base := d.ModelBase
	if base == "" {
		base = d.Model // Older drafts stored the displayed model only.
	}
	if s.modelBase == "" || s.modelBase == base {
		s.model, s.modelBase = d.Model, base
	}
}
