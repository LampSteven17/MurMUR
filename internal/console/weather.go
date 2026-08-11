package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Weather gives the room view something real to draw outside its window.
//
// The fetch lives here rather than in the browser for three reasons: the page
// is served over a strict internal origin and should not depend on an external
// host being reachable; one cached fetch serves every open tab; and the
// upstream only recomputes every 15 minutes, so polling it per-client would be
// waste with no extra freshness.
//
// Open-Meteo needs no API key and no account, which is why it is used here.
type Weather struct {
	lat, lon string

	mu      sync.Mutex
	cached  weatherOut
	fetched time.Time
	failing bool
}

type weatherOut struct {
	Code    int     `json:"code"`     // WMO weather code
	TempC   float64 `json:"temp_c"`
	IsDay   bool    `json:"is_day"`
	Wind    float64 `json:"wind_kph"`
	Updated string  `json:"updated"`
	Stale   bool    `json:"stale"` // last fetch failed; this is the previous reading
}

// NewWeather returns nil when no coordinates are configured, which disables the
// endpoint. A room with no weather draws a plain window rather than guessing.
func NewWeather(lat, lon string) *Weather {
	if lat == "" || lon == "" {
		return nil
	}
	return &Weather{lat: lat, lon: lon}
}

const weatherTTL = 12 * time.Minute

func (w *Weather) handle(rw http.ResponseWriter, _ *http.Request) {
	out, err := w.current()
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(rw).Encode(out)
}

func (w *Weather) current() (weatherOut, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.fetched) < weatherTTL && w.cached.Updated != "" {
		return w.cached, nil
	}

	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s"+
			"&current=temperature_2m,weather_code,is_day,wind_speed_10m"+
			"&wind_speed_unit=kmh&timezone=auto", w.lat, w.lon)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err == nil {
		defer resp.Body.Close()
		var body struct {
			Current struct {
				Time  string  `json:"time"`
				Temp  float64 `json:"temperature_2m"`
				Code  int     `json:"weather_code"`
				IsDay int     `json:"is_day"`
				Wind  float64 `json:"wind_speed_10m"`
			} `json:"current"`
		}
		if err = json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Current.Time != "" {
			w.cached = weatherOut{
				Code: body.Current.Code, TempC: body.Current.Temp,
				IsDay: body.Current.IsDay == 1, Wind: body.Current.Wind,
				Updated: body.Current.Time,
			}
			w.fetched = time.Now()
			w.failing = false
			return w.cached, nil
		}
	}

	// Serve the last good reading rather than nothing. A window showing
	// slightly old weather is better than a room that loses its outside world
	// because an external host had a bad minute -- but say so, so a reading
	// frozen for hours is visible as stale rather than believed.
	if w.cached.Updated != "" {
		w.failing = true
		stale := w.cached
		stale.Stale = true
		return stale, nil
	}
	return weatherOut{}, fmt.Errorf("weather unavailable: %v", err)
}
