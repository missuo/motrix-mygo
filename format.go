package main

import (
	"fmt"
	"time"
)

// formatBytes writes a size with the unit that keeps it short, as "1.5 MB".
func formatBytes(n int64) string {
	value, unit := splitBytes(n)
	return value + " " + unit
}

// splitBytes returns the number and the unit of a size apart, for texts
// that show them in different sizes.
func splitBytes(n int64) (value, unit string) {
	if n < 0 {
		return "—", ""
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	switch {
	case i == 0:
		return fmt.Sprintf("%d", n), units[i]
	case f >= 100:
		return fmt.Sprintf("%.0f", f), units[i]
	case f >= 10:
		return fmt.Sprintf("%.1f", f), units[i]
	}
	return fmt.Sprintf("%.2f", f), units[i]
}

func formatSpeed(n int64) string { return formatBytes(n) + "/s" }

// formatDuration writes a time left as "1:02:03" or "2:03".
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "–"
	}
	s := int(d.Seconds())
	if s >= 100*3600 {
		return "∞"
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// formatWhen writes a time as its hour today, else as its date.
func formatWhen(t, now time.Time) string {
	if t.IsZero() {
		return "–"
	}
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("2006-01-02")
}
