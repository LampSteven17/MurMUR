// Package events is murmur's event spine: every agent action, patrol report,
// finding, and escalation is one JSON object appended to a JSONL stream.
// Events are the source of truth; notifications (console UI, chat fanout,
// Grafana alerts) are filtered projections of this stream.
package events

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Severity levels, in increasing urgency.
const (
	SevDebug    = "debug"
	SevInfo     = "info"
	SevWarn     = "warn"
	SevEscalate = "escalate" // requires human attention; console tracks acks
)

// Well-known kinds. The set is open — new agents may add kinds freely.
const (
	KindAudit  = "audit"  // an agent invoked a tool (allowed/denied/ok/error)
	KindPatrol = "patrol" // a warden completed a patrol pass
	KindFinding = "finding"
	KindUpdate  = "update" // an unattended update ran
	KindAck     = "ack"    // a human acknowledged an escalation (ref = event id)
)

// Event is one line of the stream.
type Event struct {
	ID       string         `json:"id"`
	TS       string         `json:"ts"` // RFC3339Nano, UTC
	Agent    string         `json:"agent"`
	Severity string         `json:"severity"`
	Kind     string         `json:"kind"`
	Subject  string         `json:"subject,omitempty"`
	Message  string         `json:"message,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
	Ref      string         `json:"ref,omitempty"` // id of a related event (ack → escalation)
}

// Fill assigns ID/TS if unset and defaults severity to info.
func (e *Event) Fill() {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if e.Severity == "" {
		e.Severity = SevInfo
	}
}

// Validate rejects events that would corrupt the stream's semantics.
func (e *Event) Validate() error {
	switch e.Severity {
	case SevDebug, SevInfo, SevWarn, SevEscalate:
	default:
		return fmt.Errorf("invalid severity %q", e.Severity)
	}
	if e.Kind == "" {
		return fmt.Errorf("event kind is required")
	}
	if e.Agent == "" {
		return fmt.Errorf("event agent is required")
	}
	return nil
}

// NewID returns a 16-hex-char random event id.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Timestamp fallback keeps ids unique enough if the entropy pool
		// is somehow unavailable; never blocks the stream.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Sink accepts events. Implementations must be safe for concurrent use.
type Sink interface {
	Emit(Event) error
}

// FileSink appends events to <dir>/events-<YYYY>.jsonl (yearly rotation —
// the stream is kept forever, so rotation is by calendar year, not size).
type FileSink struct {
	mu  sync.Mutex
	dir string
}

func NewFileSink(dir string) (*FileSink, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create events dir: %w", err)
	}
	return &FileSink{dir: dir}, nil
}

func (s *FileSink) Dir() string { return s.dir }

// CurrentPath is the file events are being appended to right now.
func (s *FileSink) CurrentPath() string {
	return filepath.Join(s.dir, fmt.Sprintf("events-%d.jsonl", time.Now().UTC().Year()))
}

func (s *FileSink) Emit(e Event) error {
	e.Fill()
	if err := e.Validate(); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.CurrentPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// HTTPSink posts events to a console hub's ingest endpoint.
type HTTPSink struct {
	URL    string // base URL, e.g. http://coordinator:8686
	Client *http.Client
}

func NewHTTPSink(baseURL string) *HTTPSink {
	return &HTTPSink{URL: baseURL, Client: &http.Client{Timeout: 5 * time.Second}}
}

func (s *HTTPSink) Emit(e Event) error {
	e.Fill()
	if err := e.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	resp, err := s.Client.Post(s.URL+"/api/events", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("event hub returned %s", resp.Status)
	}
	return nil
}

// BestEffort wraps a sink so failures are swallowed (used for the network
// leg when the local file write is the guarantee).
type BestEffort struct{ Sink Sink }

func (b BestEffort) Emit(e Event) error {
	if b.Sink == nil {
		return nil
	}
	_ = b.Sink.Emit(e)
	return nil
}

// Multi fans one event out to several sinks; the first error is returned
// after all sinks have been attempted.
type Multi []Sink

func (m Multi) Emit(e Event) error {
	var first error
	for _, s := range m {
		if err := s.Emit(e); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// SystemEventsDir is the fallback used when there is no $HOME — the normal
// case for a systemd service, where agents do most of their work.
const SystemEventsDir = "/var/lib/murmur/events"

// DefaultDir resolves the local events directory: $MURMUR_EVENTS_DIR, else
// ~/.local/state/murmur/events, else SystemEventsDir.
//
// The $HOME-less fallback exists because a systemd unit without an explicit
// events dir would otherwise fail at startup ("$HOME is not defined") — an
// agent silently not running is the worst failure mode this system has.
func DefaultDir() (string, error) {
	if d := os.Getenv("MURMUR_EVENTS_DIR"); d != "" {
		return d, nil
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "murmur", "events"), nil
	}
	return SystemEventsDir, nil
}
