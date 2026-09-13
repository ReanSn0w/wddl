package main

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ReanSn0w/wddl/pkg/control"
)

const watchRecentEventLimit = 5

type watchDashboard struct {
	status control.Status
	active map[string]control.ActiveDownload
	recent []control.Event
}

func newWatchDashboard(status control.Status) *watchDashboard {
	dashboard := &watchDashboard{active: make(map[string]control.ActiveDownload)}
	dashboard.ApplyStatus(status)
	return dashboard
}

func (d *watchDashboard) ApplyStatus(status control.Status) {
	d.status = status
	d.active = make(map[string]control.ActiveDownload, len(status.Active))
	for _, active := range status.Active {
		d.active[active.ID] = active
	}
}

func (d *watchDashboard) ApplyEvent(event control.Event) {
	switch event.Type {
	case "download.started":
		active, exists := d.active[event.ID]
		active.ID = event.ID
		active.Name = event.Message
		d.active[event.ID] = active
		if !exists && d.status.Pending > 0 {
			d.status.Pending--
		}
	case "download.progress":
		if progress, ok := event.Data.(control.DownloadProgress); ok {
			active := d.active[progress.ID]
			active.ID = progress.ID
			active.Name = progress.Name
			active.Size = progress.Size
			active.Downloaded = progress.Downloaded
			active.Percent = progress.Percent
			active.Speed = progress.Speed
			if eta, ok := progressETA(progress); ok {
				active.ETA = eta
			} else {
				active.ETA = 0
			}
			d.active[progress.ID] = active
		}
	case "download.completed":
		delete(d.active, event.ID)
	case "download.cancelled":
		if _, exists := d.active[event.ID]; exists {
			d.status.Suspended++
		}
		delete(d.active, event.ID)
	case "download.failed":
		if _, exists := d.active[event.ID]; exists {
			d.status.Pending++
		}
		delete(d.active, event.ID)
	case "queue.added":
		d.status.Pending++
	case "download.resumed":
		if d.status.Suspended > 0 {
			d.status.Suspended--
		}
		d.status.Pending++
	case "scan.started":
		d.setScanRunning(event, true)
	case "scan.completed", "scan.failed":
		d.setScanRunning(event, false)
	}

	if event.Dropped > 0 || isRecentWatchEvent(event.Type) {
		d.recent = append(d.recent, event)
		if len(d.recent) > watchRecentEventLimit {
			d.recent = d.recent[len(d.recent)-watchRecentEventLimit:]
		}
	}
}

func (d *watchDashboard) setScanRunning(event control.Event, running bool) {
	kind := watchEventKind(event)
	switch kind {
	case string(control.ScanLocal):
		d.status.LocalScan.Running = running
	case string(control.ScanRemote):
		d.status.RemoteScan.Running = running
	case string(control.ScanAll):
		d.status.LocalScan.Running = running
		d.status.RemoteScan.Running = running
	}
}

func watchEventKind(event control.Event) string {
	payload, ok := event.Data.(map[string]any)
	if !ok {
		return ""
	}
	kind, _ := payload["kind"].(string)
	return kind
}

func isRecentWatchEvent(eventType string) bool {
	switch eventType {
	case "download.started", "download.completed", "download.cancelled", "download.failed",
		"scan.started", "scan.completed", "scan.failed", "config.reloaded":
		return true
	default:
		return false
	}
}

func (d *watchDashboard) Lines(width int, now time.Time) []string {
	if width <= 0 {
		width = 100
	}
	lines := []string{fitWatchLine(fmt.Sprintf("wddl watch — %d active, %d waiting, %d suspended",
		len(d.active), d.status.Pending, d.status.Suspended), width), ""}

	active := make([]control.ActiveDownload, 0, len(d.active))
	for _, item := range d.active {
		active = append(active, item)
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].Name == active[j].Name {
			return active[i].ID < active[j].ID
		}
		return active[i].Name < active[j].Name
	})
	if len(active) == 0 {
		lines = append(lines, "No active downloads")
	} else {
		for _, item := range active {
			lines = append(lines, formatActiveDownload(item, width))
		}
	}

	lines = append(lines, "", fitWatchLine(fmt.Sprintf("Remote scan: %s · Local scan: %s · Index: %d files",
		formatScanState(d.status.RemoteScan, now), formatScanState(d.status.LocalScan, now), d.status.IndexedFiles), width))
	if len(d.recent) > 0 {
		lines = append(lines, "", "Recent events:")
		for _, event := range d.recent {
			message := fmt.Sprintf("%s  %-18s %s", event.Time.Format("15:04:05"), event.Type, watchEventMessage(event))
			if event.Dropped > 0 {
				message += fmt.Sprintf(" (dropped %d; resync requested)", event.Dropped)
			}
			lines = append(lines, fitWatchLine(message, width))
		}
	}
	return lines
}

func watchEventMessage(event control.Event) string {
	message := event.Message
	if payload, ok := event.Data.(map[string]any); ok {
		if detail, ok := payload["error"].(string); ok && detail != "" {
			message += ": " + detail
		}
	}
	return message
}

func formatActiveDownload(active control.ActiveDownload, width int) string {
	percent := math.Max(0, math.Min(100, active.Percent))
	progress := control.DownloadProgress{
		Downloaded: active.Downloaded, Size: active.Size, Speed: active.Speed,
	}
	eta, hasETA := progressETA(progress)
	etaText := "ETA --"
	if hasETA {
		etaText = "ETA " + formatWatchDuration(eta)
	}

	amount := formatBytes(active.Downloaded) + "/" + formatBytes(active.Size)
	details := fmt.Sprintf("%6.1f%%  %-21s  %-12s  %s", percent, amount, formatRate(active.Speed), etaText)
	barWidth := 20
	nameWidth := width - runeLen(details) - barWidth - 6
	if nameWidth < 12 {
		details = fmt.Sprintf("%6.1f%%  %-12s", percent, formatRate(active.Speed))
		barWidth = 12
		nameWidth = width - runeLen(details) - barWidth - 6
	}
	if nameWidth < 8 {
		return fitWatchLine(fmt.Sprintf("%s  %6.1f%%", active.Name, percent), width)
	}
	if nameWidth > 36 {
		nameWidth = 36
	}

	name := padWatchText(truncateWatchText(active.Name, nameWidth), nameWidth)
	line := fmt.Sprintf("%s  [%s]  %s", name, progressBar(percent, barWidth), details)
	return fitWatchLine(line, width)
}

func progressBar(percent float64, width int) string {
	if width < 1 {
		return ""
	}
	percent = math.Max(0, math.Min(100, percent))
	filled := int(math.Round(percent / 100 * float64(width)))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatScanState(scan control.ScanState, now time.Time) string {
	switch {
	case scan.Running:
		return "running"
	case scan.Scheduled:
		return "scheduled"
	case scan.LastError != "":
		return "error"
	case scan.NextRun != nil:
		remaining := scan.NextRun.Sub(now)
		if remaining <= 0 {
			return "due"
		}
		return "next in " + formatWatchDuration(remaining)
	default:
		return "idle"
	}
}

func truncateWatchText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(value)
	return string(runes[:width-1]) + "…"
}

func fitWatchLine(value string, width int) string {
	return truncateWatchText(value, width)
}

func padWatchText(value string, width int) string {
	missing := width - runeLen(value)
	if missing <= 0 {
		return value
	}
	return value + strings.Repeat(" ", missing)
}

func runeLen(value string) int { return utf8.RuneCountInString(value) }

type ansiWatchRenderer struct {
	out           io.Writer
	width         func() int
	renderedLines int
	cursorHidden  bool
}

type interactiveWatchRenderer interface {
	Render(*watchDashboard, time.Time) error
	Close() error
}

func newANSIWatchRenderer(out io.Writer, width func() int) *ansiWatchRenderer {
	return &ansiWatchRenderer{out: out, width: width}
}

func (r *ansiWatchRenderer) Render(dashboard *watchDashboard, now time.Time) error {
	width := 100
	if r.width != nil {
		if current := r.width(); current > 0 {
			width = current
		}
	}
	lines := dashboard.Lines(width, now)
	if !r.cursorHidden {
		if _, err := io.WriteString(r.out, "\x1b[?25l"); err != nil {
			return err
		}
		r.cursorHidden = true
	}
	if r.renderedLines > 0 {
		if _, err := fmt.Fprintf(r.out, "\x1b[%dA", r.renderedLines); err != nil {
			return err
		}
	}

	rows := len(lines)
	if r.renderedLines > rows {
		rows = r.renderedLines
	}
	for index := 0; index < rows; index++ {
		line := ""
		if index < len(lines) {
			line = lines[index]
		}
		if _, err := fmt.Fprintf(r.out, "\r\x1b[2K%s\n", line); err != nil {
			return err
		}
	}
	if rows > len(lines) {
		if _, err := fmt.Fprintf(r.out, "\x1b[%dA", rows-len(lines)); err != nil {
			return err
		}
	}
	r.renderedLines = len(lines)
	return nil
}

func (r *ansiWatchRenderer) Close() error {
	if !r.cursorHidden {
		return nil
	}
	r.cursorHidden = false
	_, err := io.WriteString(r.out, "\x1b[?25h\n")
	return err
}
