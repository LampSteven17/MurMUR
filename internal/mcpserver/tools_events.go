package mcpserver

import (
	"context"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rtx-monster/murmur/internal/events"
)

// ---- events_query ----------------------------------------------------------

type EventsQueryIn struct {
	MinSeverity string `json:"min_severity,omitempty" jsonschema:"debug|info|warn|escalate — includes higher severities"`
	Kind        string `json:"kind,omitempty" jsonschema:"filter by event kind, e.g. patrol, finding, update, audit, ack"`
	Agent       string `json:"agent,omitempty" jsonschema:"filter by emitting agent, e.g. warden-patrol"`
	SinceHours  int    `json:"since_hours,omitempty" jsonschema:"only events from the last N hours"`
	Limit       int    `json:"limit,omitempty" jsonschema:"max events returned (default 100)"`
}

type EventsQueryOut struct {
	Source string         `json:"source" jsonschema:"hub URL or local directory the events came from"`
	Events []events.Event `json:"events"`
}

func (s *Server) eventsQuery(ctx context.Context, req *mcp.CallToolRequest, in EventsQueryIn) (*mcp.CallToolResult, EventsQueryOut, error) {
	var out EventsQueryOut
	q := events.Query{MinSeverity: in.MinSeverity, Kind: in.Kind, Agent: in.Agent, Limit: in.Limit}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	if in.SinceHours > 0 {
		q.Since = time.Now().Add(-time.Duration(in.SinceHours) * time.Hour)
	}

	if hub := os.Getenv("MURMUR_EVENTS_URL"); hub != "" {
		evs, err := events.QueryHTTP(hub, q)
		if err == nil {
			out.Source, out.Events = hub, evs
			return nil, out, nil
		}
		// Hub unreachable — fall through to the local file so the head
		// controller still sees this host's own slice of the spine.
	}
	dir, err := events.DefaultDir()
	if err != nil {
		return nil, out, err
	}
	evs, err := events.Read(dir, q)
	if err != nil {
		return nil, out, err
	}
	out.Source, out.Events = dir, evs
	return nil, out, nil
}
