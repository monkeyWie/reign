// Package provider is the application boundary for agent integrations.
// Native protocols and application launch details stay inside each provider.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrUnavailable = errors.New("provider unavailable")

type Capabilities struct {
	MaxAttachmentBytes int64 `json:"maxAttachmentBytes,omitempty"`
	Streaming          bool  `json:"streaming,omitempty"`
	History            bool  `json:"history"`
	Create             bool  `json:"create"`
	Send               bool  `json:"send"`
	Steer              bool  `json:"steer"`
	Interrupt          bool  `json:"interrupt"`
	Approvals          bool  `json:"approvals"`
	Desktop            bool  `json:"desktop"`
	ImageInput         bool  `json:"imageInput"`
	FileInput          bool  `json:"fileInput"`
}

type Instance struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"startedAt"`
	Path      string `json:"path"`
}

type Status struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	State          string       `json:"state"`
	Detail         string       `json:"detail"`
	Capabilities   Capabilities `json:"capabilities"`
	Instances      []Instance   `json:"instances"`
	DesktopClients int          `json:"desktopClients"`
	Endpoint       string       `json:"endpoint,omitempty"`
	Version        string       `json:"version,omitempty"`
	LastError      string       `json:"lastError,omitempty"`
	Controllable   bool         `json:"controllable"`
	BackendID      string       `json:"backendId,omitempty"`
	Installed      bool         `json:"installed"`
	Running        bool         `json:"running"`
	InstallPath    string       `json:"installPath,omitempty"`
}

// Ready requires a live protocol connection, not merely an installed binary or process.
func (s Status) Ready() bool { return s.State == "ready" || s.State == "connected" }

// AutoConnector may attach an existing Agent without launching or restarting it.
// Providers that require setup continue to expose the explicit Start/ConnectDesktop flow.
type AutoConnector interface {
	AutoConnect(context.Context) error
}

type Session struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Title      string `json:"title"`
	Preview    string `json:"preview"`
	CWD        string `json:"cwd"`
	Status     string `json:"status"`
	Source     string `json:"source"`
	// SourceKind describes the recorded creation client, independently of ProviderID
	// (the connection used to read/control this session). Source retains the raw value.
	SourceKind   string `json:"sourceKind,omitempty"`
	Originator   string `json:"originator,omitempty"`
	UpdatedAt    int64  `json:"updatedAt"`
	ActiveTurnID string `json:"activeTurnId,omitempty"`
	Model        string `json:"model,omitempty"`
}

type SessionPage struct {
	Sessions   []Session `json:"sessions"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type Item struct {
	ID               string       `json:"id"`
	Type             string       `json:"type"`
	Role             string       `json:"role"`
	Text             string       `json:"text"`
	Status           string       `json:"status,omitempty"`
	Phase            string       `json:"phase,omitempty"`
	ReasoningSummary string       `json:"reasoningSummary,omitempty"`
	Activities       []Activity   `json:"activities,omitempty"`
	DurationMS       *int64       `json:"durationMs,omitempty"`
	TimestampMS      *int64       `json:"timestampMs,omitempty"`
	TimestampSource  string       `json:"timestampSource,omitempty"`
	Attachments      []Attachment `json:"attachments,omitempty"`
}

// Attachment is a session-scoped resource, never a remotely supplied file path.
type Attachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Thumbnail bool   `json:"thumbnail,omitempty"`
	Source    string `json:"source,omitempty"`
	Inline    bool   `json:"inline,omitempty"`
}

// AttachmentSender is optional; providers map stored resources to native input.
type AttachmentSender interface {
	SendAttachments(context.Context, string, string, string, []string) error
	SteerAttachments(context.Context, string, string, string, []string) error
}

// Activity is presentation metadata, not a tool's raw output or model reasoning.
// Providers map their native tool actions into this common vocabulary.
type Activity struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Items  []Item `json:"items"`
}

type Transcript struct {
	StreamEpoch  string  `json:"streamEpoch,omitempty"`
	StreamCursor int64   `json:"streamCursor,omitempty"`
	Session      Session `json:"session"`
	Turns        []Turn  `json:"turns"`
	NextCursor   string  `json:"nextCursor,omitempty"`
}

type CreateOptions struct {
	CWD   string `json:"cwd"`
	Model string `json:"model,omitempty"`
}

type Model struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// SessionModelProvider discovers models in the selected workspace/session.
// Agents may advertise different choices per session; do not borrow another
// session's list when composing a prompt.
type SessionModelProvider interface {
	SessionModels(context.Context, string) ([]Model, error)
}

// ManagedProvider owns a subprocess runtime and remembers explicit enablement.
type ManagedProvider interface{ Managed() bool }

// ModelProvider is optional: providers without model selection retain Send.
type ModelProvider interface {
	Models(context.Context) ([]Model, error)
	SendWithModel(context.Context, string, string, string) error
}

type Approval struct {
	ID         string          `json:"id"`
	ProviderID string          `json:"providerId"`
	SessionID  string          `json:"sessionId"`
	TurnID     string          `json:"turnId"`
	Method     string          `json:"method"`
	Params     json.RawMessage `json:"params"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type Decision struct {
	Decision string `json:"decision"`
	Answers  map[string]struct {
		Answers []string `json:"answers"`
	} `json:"answers,omitempty"`
}

type Provider interface {
	Status(context.Context) Status
	Start(context.Context) error
	Close() error
	Sessions(context.Context, string) (SessionPage, error)
	Transcript(context.Context, string, string) (Transcript, error)
	Create(context.Context, CreateOptions) (Session, error)
	Send(context.Context, string, string) error
	Steer(context.Context, string, string, string) error
	Interrupt(context.Context, string, string) error
	Approvals() []Approval
	Decide(context.Context, string, Decision) error
	// Expected contains the exact instances the user saw when confirming.
	ConnectDesktop(context.Context, []Instance, bool) error
}
