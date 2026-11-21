package persistence

import (
	"context"
	"maps"
)

type ReadKind string

const (
	ProviderList ReadKind = "providers"
	ModelList    ReadKind = "models"
	ApprovalList ReadKind = "approvals"
)

func (k ReadKind) Table() string {
	switch k {
	case ProviderList:
		return "providers"
	case ModelList:
		return "models"
	case ApprovalList:
		return "approval_previews"
	}
	return ""
}

type ReadDAO interface {
	SaveList(context.Context, Session, ReadKind, []Object) error
	LoadList(context.Context, Session, ReadKind) ([]Object, error)
}

func (k ReadKind) Record(v Object) Object {
	if k != ApprovalList {
		return v
	}
	a := maps.Clone(v)
	delete(a, "params")
	delete(a, "questions")
	return a
}
