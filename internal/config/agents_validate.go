package config

import (
	"fmt"
	"strings"
	"time"
)

// validateAgents checks the murmuration rulespace. Loud-fail: a misdeclared
// grant must never silently widen into "no grant checked".
func (c *Config) validateAgents() []string {
	var errs []string
	seen := map[string]bool{}
	appByName := map[string]App{}
	for _, a := range c.Apps {
		appByName[a.Name] = a
	}
	userNames := map[string]bool{}
	for _, u := range c.Users {
		userNames[u.Name] = true
	}

	for i, ag := range c.Agents {
		if ag.Name == "" {
			errs = append(errs, fmt.Sprintf("agents[%d].name is required", i))
			continue
		}
		if seen[ag.Name] {
			errs = append(errs, fmt.Sprintf("agents[%d].name %q is duplicated", i, ag.Name))
		}
		seen[ag.Name] = true

		for _, appName := range ag.UnattendedApps {
			app, ok := appByName[appName]
			if !ok {
				errs = append(errs, fmt.Sprintf("agents[%s].unattended_apps: %q is not in the apps: catalog", ag.Name, appName))
				continue
			}
			if app.Update == "" {
				errs = append(errs, fmt.Sprintf("agents[%s].unattended_apps: app %q has no update: command", ag.Name, appName))
			}
		}

		if len(ag.UnattendedApps) > 0 {
			// An agent with mutation grants needs a real identity to act as.
			if len(c.Users) > 0 && !userNames[ag.Name] {
				errs = append(errs, fmt.Sprintf("agents[%s] has unattended_apps but no matching users: entry to act as", ag.Name))
			}
			if ag.UpdateWindow == "" {
				errs = append(errs, fmt.Sprintf("agents[%s] has unattended_apps but no update_window — unattended updates require a window", ag.Name))
			}
		}

		if ag.UpdateWindow != "" {
			if _, _, err := ParseWindow(ag.UpdateWindow); err != nil {
				errs = append(errs, fmt.Sprintf("agents[%s].update_window: %v", ag.Name, err))
			}
		}
	}
	return errs
}

// ParseWindow parses "HH:MM-HH:MM" into start/end minutes-of-day.
func ParseWindow(w string) (start, end int, err error) {
	parts := strings.SplitN(w, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("want \"HH:MM-HH:MM\", got %q", w)
	}
	toMin := func(s string) (int, error) {
		t, err := time.Parse("15:04", strings.TrimSpace(s))
		if err != nil {
			return 0, fmt.Errorf("bad time %q in window", s)
		}
		return t.Hour()*60 + t.Minute(), nil
	}
	if start, err = toMin(parts[0]); err != nil {
		return 0, 0, err
	}
	if end, err = toMin(parts[1]); err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

// InWindow reports whether the given local time falls inside the window
// (handles windows that cross midnight).
func InWindow(w string, now time.Time) (bool, error) {
	start, end, err := ParseWindow(w)
	if err != nil {
		return false, err
	}
	m := now.Hour()*60 + now.Minute()
	if start <= end {
		return m >= start && m < end, nil
	}
	return m >= start || m < end, nil // crosses midnight
}

// AgentByName returns the agents: entry with the given name.
func (c *Config) AgentByName(name string) (Agent, bool) {
	for _, a := range c.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}
