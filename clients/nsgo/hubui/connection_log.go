package hubui

import (
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

const diagnosticLimit = 300

// Export only connection metadata. Pairing params, commands and conversations
// must never be serialized into a diagnostic report.
var diagnosticURL = regexp.MustCompile(`(?i)(?:https?|wss?)://[^\s<>"']+`)
var diagnosticSecret = regexp.MustCompile(`(?i)("?(?:token|password|secret|authorization|key|grant)"?\s*[:=]\s*)(?:"[^"\r\n]*"|[^\s,;]+)`)
var diagnosticBearer = regexp.MustCompile(`(?i)Bearer\s+[^\s,;]+`)

func safeDiagnostic(value string) string {
	value = diagnosticURL.ReplaceAllStringFunc(value, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[redacted URL]"
		}
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		return u.String()
	})
	value = diagnosticBearer.ReplaceAllString(value, "Bearer [redacted]")
	value = diagnosticSecret.ReplaceAllString(value, "${1}[redacted]")
	if len(value) > 4096 {
		value = value[:4096] + "…"
	}
	return value
}
func appendDiagnostic(events []connectionStep, p connectivity.Progress) []connectionStep {
	p.Message, p.Detail = safeDiagnostic(p.Message), safeDiagnostic(p.Detail)
	if len(events) > 0 && events[len(events)-1].Progress == p {
		return events
	}
	events = append(events, connectionStep{Progress: p, at: time.Now()})
	if len(events) > diagnosticLimit {
		events = append([]connectionStep(nil), events[len(events)-diagnosticLimit:]...)
	}
	return events
}
func (s *State) logConnection(id, stage, message, detail, state string) {
	h := s.connection(id)
	h.diagnostics = appendDiagnostic(h.diagnostics, connectivity.Progress{Stage: stage, Message: message, Detail: detail, State: state})
}
func (s *State) observeTransports() {
	for _, d := range s.devices {
		id := str(d, "id")
		h := s.connection(id)
		state := fmt.Sprintf("connected=%t path=%s", flag(d, "connected"), str(d, "path"))
		if !h.observed || h.lastTransport != state {
			message, level := "传输连接已就绪", "success"
			if !flag(d, "connected") {
				message, level = "传输连接已断开", "warning"
			}
			s.logConnection(id, "transport", message, state, level)
			h.observed, h.lastTransport = true, state
		}
	}
}
func (s *State) connectionLog(id string, a *connectAttempt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ReignHub connection diagnostics\nRuntime: %s/%s %s\nClient: %s\nDevice: %s\nStatus: %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), s.clientName, id, s.connectionTitle(id))
	if id == "" && s.api.CanHost() {
		network := object(s.host["network"])
		fmt.Fprintf(&b, "Host: %s\nName: %s\nService: %s\nHost transport: %s\n", safeDiagnostic(str(s.host, "deviceId")), safeDiagnostic(str(s.host, "name")), safeDiagnostic(str(s.host, "serviceUrl")), safeDiagnostic(str(network, "state")))
	}
	if detail := s.connectionDetails(id); detail != "" {
		fmt.Fprintf(&b, "Details: %s\n", safeDiagnostic(detail))
	}
	for _, d := range s.devices {
		if str(d, "id") == id {
			fmt.Fprintf(&b, "Name: %s\nService: %s\nTransport: connected=%t path=%s\n", safeDiagnostic(deviceName(d)), safeDiagnostic(str(d, "service")), flag(d, "connected"), str(d, "path"))
		}
	}
	events := append([]connectionStep(nil), s.connection(id).diagnostics...)
	if id != "" {
		events = append(events, s.connection("").diagnostics...)
	}
	if a != nil {
		fmt.Fprintf(&b, "Endpoint: %s\nAttempt: active=%t complete=%t\n", safeDiagnostic(str(a.params, "service")), a.active, a.complete)
		if !a.started.IsZero() {
			fmt.Fprintf(&b, "Attempt started: %s\n", s.localTime(a.started).Format(time.RFC3339Nano))
		}
		if !a.finished.IsZero() {
			fmt.Fprintf(&b, "Attempt ended: %s\n", s.localTime(a.finished).Format(time.RFC3339Nano))
		}
		events = append(events, a.events...)
		if a.err != "" {
			fmt.Fprintf(&b, "Attempt error: %s\n", safeDiagnostic(a.err))
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	if len(events) > diagnosticLimit {
		events = events[len(events)-diagnosticLimit:]
	}
	for _, e := range events {
		fmt.Fprintf(&b, "%s [%s/%s] %s", s.localTime(e.at).Format(time.RFC3339Nano), e.Stage, e.State, e.Message)
		if e.Detail != "" {
			fmt.Fprintf(&b, " | %s", e.Detail)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
