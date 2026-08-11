// Package console is murmur's self-hosted notification hub: it ingests
// events from every agent (POST /api/events), appends them to the canonical
// JSONL stream, and serves a live web UI + SSE feed + query API. It is the
// single always-on URL a human checks; chat integrations are optional fanout
// layered on top, never the primary channel.
package console

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/rtx-monster/murmur/internal/events"
)

//go:embed ui.html
var uiHTML []byte

//go:embed room.js
var roomJS []byte

// Vendored rather than pulled from a CDN: the console has to render on a lab
// network that may have no route off-site. anime.js v4.5.0, MIT.
//
//go:embed anime.umd.min.js
var animeJS []byte

// Server is the event hub, plus an optional live fleet view.
type Server struct {
	sink    *events.FileSink
	fleet   *Fleet   // nil when the console runs credential-free (no --config)
	weather *Weather // nil unless coordinates are configured

	mu   sync.Mutex
	subs map[chan events.Event]struct{}
}

func New(eventsDir string) (*Server, error) {
	sink, err := events.NewFileSink(eventsDir)
	if err != nil {
		return nil, err
	}
	return &Server{sink: sink, subs: map[chan events.Event]struct{}{}}, nil
}

// SetFleet enables the fleet view. Pass a read-only PVE identity — the console
// renders cluster state and never mutates it.
func (s *Server) SetFleet(f *Fleet) { s.fleet = f }

// SetWeather enables the room's window. Pass nil to leave it a plain view.
func (s *Server) SetWeather(w *Weather) { s.weather = w }

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context, listen string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleUI)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("GET /room.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		// Without this the browser picks a heuristic freshness lifetime and an
		// already-open dashboard keeps running whatever room.js it first saw --
		// a deployed fix simply never arrives. no-cache still allows a 304, it
		// only forbids reusing the copy without asking.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(roomJS)
	})
	mux.HandleFunc("GET /anime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		_, _ = w.Write(animeJS)
	})
	mux.HandleFunc("POST /api/events", s.handleIngest)
	mux.HandleFunc("GET /api/events", s.handleQuery)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	mux.HandleFunc("POST /api/ack", s.handleAck)
	mux.HandleFunc("GET /api/weather", func(w http.ResponseWriter, r *http.Request) {
		if s.weather == nil {
			http.Error(w, "weather disabled: no coordinates configured", http.StatusNotFound)
			return
		}
		s.weather.handle(w, r)
	})
	mux.HandleFunc("GET /api/fleet", func(w http.ResponseWriter, r *http.Request) {
		if s.fleet == nil {
			http.Error(w, "fleet view disabled: console started without --config", http.StatusNotFound)
			return
		}
		s.fleet.handleFleet(w, r)
	})

	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("murmur console: listening on %s, events in %s", listen, s.sink.Dir())
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(uiHTML)
}

// accept appends an event to the canonical stream and broadcasts it.
func (s *Server) accept(e *events.Event) error {
	e.Fill()
	if err := e.Validate(); err != nil {
		return err
	}
	if err := s.sink.Emit(*e); err != nil {
		return err
	}
	s.mu.Lock()
	for ch := range s.subs {
		select {
		case ch <- *e:
		default: // slow subscriber loses live events; the log has the truth
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var e events.Event
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&e); err != nil {
		http.Error(w, "bad event json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.accept(&e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": e.ID})
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	q := events.Query{
		MinSeverity: r.URL.Query().Get("min_severity"),
		Kind:        r.URL.Query().Get("kind"),
		Agent:       r.URL.Query().Get("agent"),
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &q.Limit)
	}
	if v := r.URL.Query().Get("since"); v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "bad since (want RFC3339)", http.StatusBadRequest)
			return
		}
		q.Since = ts
	}
	evs, err := events.Read(s.sink.Dir(), q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"events": evs})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan events.Event, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, ": murmur console stream\n\n")
	fl.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			fl.Flush()
		case e := <-ch:
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			fl.Flush()
		}
	}
}

// handleAck records a human acknowledgment of an escalation as an event —
// append-only state, no database.
func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ref string `json:"ref"`
		By  string `json:"by,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.Ref == "" {
		http.Error(w, "want {\"ref\": \"<event id>\"}", http.StatusBadRequest)
		return
	}
	if in.By == "" {
		in.By = "console"
	}
	e := events.Event{
		Agent:    "console:" + in.By,
		Severity: events.SevInfo,
		Kind:     events.KindAck,
		Ref:      in.Ref,
		Message:  "escalation acknowledged",
	}
	if err := s.accept(&e); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": e.ID})
}
