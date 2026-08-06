package events

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// QueryHTTP fetches events from a console hub's /api/events endpoint.
func QueryHTTP(baseURL string, q Query) ([]Event, error) {
	v := url.Values{}
	if q.MinSeverity != "" {
		v.Set("min_severity", q.MinSeverity)
	}
	if q.Kind != "" {
		v.Set("kind", q.Kind)
	}
	if q.Agent != "" {
		v.Set("agent", q.Agent)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if !q.Since.IsZero() {
		v.Set("since", q.Since.UTC().Format(time.RFC3339))
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(baseURL + "/api/events?" + v.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("event hub returned %s", resp.Status)
	}
	var out struct {
		Events []Event `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Events, nil
}
