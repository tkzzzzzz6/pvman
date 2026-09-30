package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPrimary   = lipgloss.Color("#7C3AED")
	colorGreen     = lipgloss.Color("#10B981")
	colorMuted     = lipgloss.Color("#6B7280")
	colorAmber     = lipgloss.Color("#F59E0B")
	colorRed       = lipgloss.Color("#EF4444")
	colorText      = lipgloss.Color("#F9FAFB")
	colorBorder    = lipgloss.Color("#374151")
	colorDarkBg    = lipgloss.Color("#1F2937")
	colorCondaBlue = lipgloss.Color("#3B82F6")

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	panelActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary)

	sectionHeaderStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				PaddingLeft(1).
				Bold(true)

	itemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(colorText).
				Background(colorPrimary)

	activeMarkerStyle = lipgloss.NewStyle().
				Foreground(colorAmber)

	detailKeyStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(10)

	detailValStyle = lipgloss.NewStyle().
			Foreground(colorText)

	detailTitleStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	condaBadgeStyle = lipgloss.NewStyle().
			Foreground(colorCondaBlue).
			Bold(true)

	uvBadgeStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	activeCmdStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Background(colorDarkBg).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	successStyle = lipgloss.NewStyle().
			Foreground(colorGreen)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	keyStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	inputLabelStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	confirmStyle = lipgloss.NewStyle().
			Foreground(colorAmber).
			Bold(true)

	// filterStatusStyle marks a list that is showing a subset, so the missing
	// rows read as a choice the user made rather than a failed load.
	filterStatusStyle = lipgloss.NewStyle().
				Foreground(colorCondaBlue).
				Bold(true)

	dangerStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(colorPrimary)
)

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatAge renders a creation time as its date plus how long ago that was,
// e.g. "2025-12-06 (9mo ago)". A zero time means it could not be determined.
func formatAge(t, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := now.Sub(t)
	var ago string
	switch {
	case d < time.Minute: // includes a clock that runs behind the file's
		ago = "just now"
	case d < time.Hour:
		ago = fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		ago = fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 30*24*time.Hour:
		ago = fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	case d < 365*24*time.Hour:
		ago = fmt.Sprintf("%dmo ago", int(d/(30*24*time.Hour)))
	default:
		ago = fmt.Sprintf("%dy ago", int(d/(365*24*time.Hour)))
	}
	return t.Local().Format("2006-01-02") + " (" + ago + ")"
}
