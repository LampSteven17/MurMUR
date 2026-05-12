package tui

import (
	"fmt"
	"time"
)

// formatBytes returns a short human-readable byte count.
// 1.5GB → "1.5G", 256GB → "256G". Returns "—" for zero.
func formatBytes(n int64) string {
	if n <= 0 {
		return "—"
	}
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)
	switch {
	case n >= tib:
		return fmt.Sprintf("%.1fT", float64(n)/float64(tib))
	case n >= gib:
		v := float64(n) / float64(gib)
		if v >= 100 {
			return fmt.Sprintf("%.0fG", v)
		}
		return fmt.Sprintf("%.1fG", v)
	case n >= mib:
		return fmt.Sprintf("%.0fM", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.0fK", float64(n)/float64(kib))
	}
	return fmt.Sprintf("%dB", n)
}

// formatUptime returns a compact uptime: "11d 4h", "0h 12m", "—".
func formatUptime(secs int64) string {
	if secs <= 0 {
		return "—"
	}
	d := time.Duration(secs) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// formatPercent renders a fraction (0..1) as "12%".
func formatPercent(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d%%", int(v*100+0.5))
}
