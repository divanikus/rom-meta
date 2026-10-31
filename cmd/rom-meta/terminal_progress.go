package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"rommeta/internal/catalog"
	"rommeta/internal/taskprogress"
)

type terminalProgress struct {
	transferLine       string
	lastTransferReport time.Time
	out                *os.File
	interactive        bool
	color              bool
	lastLineCount      int
	byteItems          map[string]byteProgressItem
	lastByteReads      map[string]int64
	taskLine           string
	compactTask        bool
	mu                 sync.Mutex
}

type byteProgressItem struct {
	Task    string
	Stage   string
	Label   string
	Worker  int
	Workers int
	Current int64
	Total   int64
	Started time.Time
}

func newTerminalProgress(out *os.File) *terminalProgress {
	progress := &terminalProgress{out: out}
	if out == nil {
		return progress
	}
	progress.interactive = isInteractiveTerminal(out)
	progress.color = progress.interactive && os.Getenv("NO_COLOR") == ""
	return progress
}

func (p *terminalProgress) UpdateTask(task, stage string, current, total int, label string) {
	if p == nil || p.out == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.interactive {
		if total <= 0 || current == 0 || current == 1 || current == total || current%25 == 0 {
			if strings.TrimSpace(stage) == "" {
				fmt.Fprintf(p.out, "progress: %s %d/%d | %s\n", task, current, total, label)
			} else {
				fmt.Fprintf(p.out, "progress: %s %s %d/%d | %s\n", task, stage, current, total, label)
			}
		}
		return
	}

	if p.compactTask {
		p.taskLine = formatCompactTaskProgressLine(task, stage, current, total, label, p.color)
	} else {
		p.taskLine = formatTaskProgressLine(task, stage, current, total, label, p.color)
	}
	p.renderLines(p.progressLines())
}

func (p *terminalProgress) UpdateTaskBytes(task, stage string, current, total int64, label string) {
	if p == nil || p.out == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := task + "\x00" + stage + "\x00" + label
	if current < 0 && total < 0 {
		delete(p.lastByteReads, key)
		delete(p.byteItems, key)
		if p.interactive {
			p.renderLines(p.progressLines())
		}
		return
	}
	if !p.interactive {
		if p.shouldReportBytes(key, current, total) {
			if total > 0 {
				percent := int64(0)
				if total > 0 {
					percent = current * 100 / total
				}
				fmt.Fprintf(p.out, "progress: %s %s %s/%s %3d%% | %s\n", task, stage, formatBytes(current), formatBytes(total), percent, label)
			} else {
				fmt.Fprintf(p.out, "progress: %s %s downloaded=%s | %s\n", task, stage, formatBytes(current), label)
			}
		}
		return
	}

	if p.byteItems == nil {
		p.byteItems = map[string]byteProgressItem{}
	}
	item := byteProgressItem{
		Task:    task,
		Stage:   stage,
		Label:   label,
		Current: current,
		Total:   total,
	}
	p.byteItems[key] = item
	p.renderLines(p.progressLines())
	if total > 0 && current >= total {
		delete(p.byteItems, key)
	}
}

func (p *terminalProgress) UpdateWorker(task, stage string, worker, workers int, label string) {
	if p == nil || p.out == nil || worker <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := workerProgressKey(task, worker)
	if p.byteItems == nil {
		p.byteItems = map[string]byteProgressItem{}
	}
	started := time.Now()
	if previous, ok := p.byteItems[key]; ok && previous.Stage == stage && previous.Label == label && !previous.Started.IsZero() {
		started = previous.Started
	}
	p.byteItems[key] = byteProgressItem{
		Task:    task,
		Stage:   stage,
		Label:   label,
		Worker:  worker,
		Workers: workers,
		Started: started,
	}
	if p.interactive {
		p.renderLines(p.progressLines())
	}
}

func (p *terminalProgress) UpdateWorkerBytes(task, stage string, worker, workers int, current, total int64, label string) {
	if p == nil || p.out == nil || worker <= 0 {
		return
	}
	key := workerProgressKey(task, worker)
	if current < 0 && total < 0 {
		p.UpdateWorker(task, "waiting", worker, workers, "waiting...")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.interactive {
		if p.shouldReportBytes(key, current, total) {
			if total > 0 {
				percent := current * 100 / total
				fmt.Fprintf(p.out, "progress: %s worker=%d %s %s/%s %3d%% | %s\n", task, worker, stage, formatBytes(current), formatBytes(total), percent, label)
			} else {
				fmt.Fprintf(p.out, "progress: %s worker=%d %s downloaded=%s | %s\n", task, worker, stage, formatBytes(current), label)
			}
		}
		return
	}
	if p.byteItems == nil {
		p.byteItems = map[string]byteProgressItem{}
	}
	started := time.Now()
	if previous, ok := p.byteItems[key]; ok && previous.Label == label && !previous.Started.IsZero() {
		started = previous.Started
	}
	item := byteProgressItem{
		Task:    task,
		Stage:   stage,
		Label:   label,
		Worker:  worker,
		Workers: workers,
		Current: current,
		Total:   total,
		Started: started,
	}
	p.byteItems[key] = item
	p.renderLines(p.progressLines())
	if total > 0 && current >= total {
		p.byteItems[key] = byteProgressItem{
			Task:    task,
			Stage:   "waiting",
			Label:   "waiting...",
			Worker:  worker,
			Workers: workers,
		}
	}
}

func (p *terminalProgress) progressLines() []string {
	byteLines := p.byteProgressLines()
	if p.transferLine != "" {
		byteLines = append([]string{p.transferLine}, byteLines...)
	}
	if p.taskLine == "" {
		return byteLines
	}
	lines := make([]string, 0, len(byteLines)+1)
	lines = append(lines, p.taskLine)
	lines = append(lines, byteLines...)
	return lines
}

func (p *terminalProgress) shouldReportBytes(key string, current, total int64) bool {
	if p.lastByteReads == nil {
		p.lastByteReads = map[string]int64{}
	}
	if current <= 0 {
		p.lastByteReads[key] = current
		return true
	}
	if total > 0 && current >= total {
		delete(p.lastByteReads, key)
		return true
	}
	step := int64(5 * 1024 * 1024)
	if total > 0 {
		if computed := total / 20; computed > step {
			step = computed
		}
	}
	if current-p.lastByteReads[key] >= step {
		p.lastByteReads[key] = current
		return true
	}
	return false
}

func (p *terminalProgress) byteProgressLines() []string {
	keys := make([]string, 0, len(p.byteItems))
	for key := range p.byteItems {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		item := p.byteItems[key]
		if item.Worker > 0 {
			lines = append(lines, formatWorkerProgressLine(item, p.color))
		} else {
			lines = append(lines, formatTaskBytesProgressLine(item.Task, item.Stage, item.Current, item.Total, item.Label, p.color))
		}
	}
	return lines
}

func workerProgressKey(task string, worker int) string {
	return fmt.Sprintf("%s\x00%04d", task, worker)
}

func (p *terminalProgress) render(line string) {
	p.renderLines([]string{line})
}

func (p *terminalProgress) renderLines(lines []string) {
	if len(lines) == 0 && p.lastLineCount == 0 {
		return
	}
	// A fresh renderer may inherit a cursor left after another progress stage.
	// Always own the entire row; padding can itself wrap at the terminal edge.
	fmt.Fprint(p.out, "\r")
	for i := 1; i < p.lastLineCount; i++ {
		fmt.Fprint(p.out, "\x1b[1A\r")
	}
	drawCount := len(lines)
	if p.lastLineCount > drawCount {
		drawCount = p.lastLineCount
	}
	for index := 0; index < drawCount; index++ {
		fmt.Fprint(p.out, "\x1b[2K")
		if index < len(lines) {
			fmt.Fprint(p.out, lines[index])
		}
		if index < drawCount-1 {
			fmt.Fprint(p.out, "\r\n")
		}
	}
	if extra := drawCount - len(lines); extra > 0 {
		fmt.Fprint(p.out, "\r")
		for i := 0; i < extra; i++ {
			fmt.Fprint(p.out, "\x1b[1A")
		}
	}
	p.lastLineCount = len(lines)
}

func (p *terminalProgress) Done() {
	if p == nil || p.out == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byteItems = nil
	p.lastByteReads = nil
	p.taskLine = ""
	if !p.interactive || p.lastLineCount == 0 {
		return
	}
	fmt.Fprint(p.out, "\r\n")
	p.lastLineCount = 0
}

func formatTaskProgressLine(task, stage string, current, total int, label string, color bool) string {
	if total <= 0 {
		if strings.TrimSpace(stage) == "" {
			return fmt.Sprintf(
				"%s | %s",
				colorizeIf(task, ansiCyan, color),
				trimProgressText(label, 70),
			)
		}
		return fmt.Sprintf(
			"%s | %s | %s",
			colorizeIf(task, ansiCyan, color),
			colorizeIf(stage, ansiYellow, color),
			trimProgressText(label, 70),
		)
	}
	percent := current * 100 / total
	bar := progressBar(current, total, 28)
	if color {
		bar = colorize(bar, ansiGreen)
	}
	if strings.TrimSpace(stage) == "" {
		return fmt.Sprintf(
			"%s [%s] %d/%d %3d%% | %s",
			colorizeIf(task, ansiCyan, color),
			bar,
			current,
			total,
			percent,
			trimProgressText(label, 54),
		)
	}
	return fmt.Sprintf(
		"%s [%s] %d/%d %3d%% | %s | %s",
		colorizeIf(task, ansiCyan, color),
		bar,
		current,
		total,
		percent,
		colorizeIf(stage, ansiYellow, color),
		trimProgressText(label, 54),
	)
}

func formatCompactTaskProgressLine(task, stage string, current, total int, label string, color bool) string {
	if total <= 0 {
		parts := []string{colorizeIf(task, ansiCyan, color)}
		if strings.TrimSpace(stage) != "" {
			parts = append(parts, colorizeIf(stage, ansiYellow, color))
		}
		if strings.TrimSpace(label) != "" {
			parts = append(parts, trimProgressText(label, 70))
		}
		return strings.Join(parts, " | ")
	}
	percent := current * 100 / total
	bar := progressBar(current, total, 28)
	if color {
		bar = colorize(bar, ansiGreen)
	}
	if strings.TrimSpace(stage) == "" {
		return fmt.Sprintf(
			"%s [%s] %d/%d %3d%% | %s",
			colorizeIf(task, ansiCyan, color),
			bar,
			current,
			total,
			percent,
			trimProgressText(label, 64),
		)
	}
	return fmt.Sprintf(
		"%s [%s] %d/%d %3d%% | %s | %s",
		colorizeIf(task, ansiCyan, color),
		bar,
		current,
		total,
		percent,
		colorizeIf(stage, ansiYellow, color),
		trimProgressText(label, 64),
	)
}

func formatTaskBytesProgressLine(task, stage string, current, total int64, label string, color bool) string {
	if total <= 0 {
		return fmt.Sprintf(
			"%s | %s | %s=%s | %s",
			colorizeIf(task, ansiCyan, color),
			colorizeIf(stage, ansiYellow, color),
			colorizeIf("downloaded", ansiGreen, color),
			formatBytes(current),
			trimProgressText(label, 50),
		)
	}
	percent := int64(0)
	if total > 0 {
		percent = current * 100 / total
	}
	bar := progressBar64(current, total, 28)
	if color {
		bar = colorize(bar, ansiGreen)
	}
	return fmt.Sprintf(
		"%s [%s] %s/%s %3d%% | %s | %s",
		colorizeIf(task, ansiCyan, color),
		bar,
		formatBytes(current),
		formatBytes(total),
		percent,
		colorizeIf(stage, ansiYellow, color),
		trimProgressText(label, 50),
	)
}

func formatWorkerProgressLine(item byteProgressItem, color bool) string {
	worker := fmt.Sprintf("#%d", item.Worker)
	if item.Stage == "querying" || item.Stage == "rate limit" || item.Stage == "writing metadata" {
		elapsed := time.Duration(0)
		if !item.Started.IsZero() {
			elapsed = time.Since(item.Started).Truncate(time.Second)
		}
		return fmt.Sprintf("%s %s | %s | %s",
			colorizeIf(worker, ansiCyan, color),
			formatWorkerStatusActivity(workerSpinner(item)+" "+item.Stage, color),
			elapsed, trimProgressText(item.Label, 100))
	}
	if strings.TrimSpace(item.Stage) == "" || item.Stage == "waiting" {
		return fmt.Sprintf(
			"%s [%s] %s",
			colorizeIf(worker, ansiCyan, color),
			progressBar(0, 0, 28),
			formatWorkerStatusActivity(workerSpinner(item)+" waiting...", color),
		)
	}
	if item.Stage == "preparing" || (item.Stage == "downloading" && item.Current <= 0 && item.Total <= 0) {
		return fmt.Sprintf(
			"%s [%s] %s | %s",
			colorizeIf(worker, ansiCyan, color),
			progressBar(0, 0, 28),
			formatWorkerStatusActivity(workerSpinner(item)+" preparing...", color),
			trimProgressText(item.Label, 72),
		)
	}
	speed := formatSpeed(item.Current, item.Started)
	if item.Total <= 0 {
		return fmt.Sprintf(
			"%s [%s] %s | %s",
			colorizeIf(worker, ansiCyan, color),
			progressBar(0, 0, 28),
			formatUnknownWorkerActivity(formatBytes(item.Current), speed, color),
			trimProgressText(item.Label, 72),
		)
	}
	bar := progressBar64(item.Current, item.Total, 28)
	if color {
		bar = colorize(bar, ansiGreen)
	}
	return fmt.Sprintf(
		"%s [%s] %s | %s",
		colorizeIf(worker, ansiCyan, color),
		bar,
		formatKnownWorkerActivity(formatBytes(item.Current), speed, color),
		trimProgressText(item.Label, 72),
	)
}

const (
	alignLeft = iota
	alignRight

	workerSizeWidth     = 9
	workerSpeedWidth    = 12
	workerActivityWidth = workerSizeWidth + workerSpeedWidth + 3
)

func formatWorkerStatusActivity(status string, color bool) string {
	return colorizeIf(status, ansiYellow, color)
}

func formatUnknownWorkerActivity(current, speed string, color bool) string {
	current = fixedWidth(current, workerSizeWidth, alignRight)
	speed = fixedWidth(speed, workerSpeedWidth, alignRight)
	return colorizeIf(current, ansiGreen, color) + " (" + colorizeIf(speed, ansiCyan, color) + ")"
}

func formatKnownWorkerActivity(current, speed string, color bool) string {
	return formatUnknownWorkerActivity(current, speed, color)
}

func fixedWidth(value string, width int, align int) string {
	if len(value) >= width {
		return value
	}
	padding := strings.Repeat(" ", width-len(value))
	if align == alignLeft {
		return value + padding
	}
	return padding + value
}

func workerSpinner(item byteProgressItem) string {
	const frames = `|/-\`
	if item.Started.IsZero() {
		return string(frames[0])
	}
	index := int(time.Since(item.Started)/(125*time.Millisecond)) % len(frames)
	return string(frames[index])
}

func formatSpeed(bytes int64, started time.Time) string {
	if bytes <= 0 || started.IsZero() {
		return "0 B/s"
	}
	elapsed := time.Since(started).Seconds()
	if elapsed < 0.1 {
		elapsed = 0.1
	}
	return formatBytes(int64(float64(bytes)/elapsed)) + "/s"
}

func validateProgressStage(snapshot catalog.ValidateProgress) string {
	if snapshot.Stage == "validating" {
		return ""
	}
	return snapshot.Stage
}

func validateProgressLabel(snapshot catalog.ValidateProgress) string {
	switch {
	case snapshot.Platform != "" && snapshot.Game != "":
		return snapshot.Platform + " / " + snapshot.Game
	case snapshot.Game != "":
		return snapshot.Game
	case snapshot.Platform != "":
		return snapshot.Platform
	default:
		return snapshot.Stage
	}
}

func progressBar(current, total, width int) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat("-", width)
	}
	filled := current * width / total
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("=", filled) + strings.Repeat("-", width-filled)
}

func progressBar64(current, total int64, width int) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat("-", width)
	}
	filled := int(current * int64(width) / total)
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("=", filled) + strings.Repeat("-", width-filled)
}

func formatBytes(value int64) string {
	if value < 0 {
		value = 0
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	scaled := float64(value)
	unit := 0
	for scaled >= 1024 && unit < len(units)-1 {
		scaled /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", scaled, units[unit])
}

func trimProgressText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func runeWidth(value string) int {
	width := 0
	inEscape := false
	inCSI := false
	for _, r := range value {
		if inEscape {
			if !inCSI && r == '[' {
				inCSI = true
				continue
			}
			if inCSI {
				if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
					inEscape = false
					inCSI = false
				}
				continue
			}
			if r >= '@' && r <= '~' {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		width++
	}
	return width
}

func colorizeIf(value, color string, enabled bool) string {
	if !enabled {
		return value
	}
	return colorize(value, color)
}

func colorize(value, color string) string {
	return color + value + ansiReset
}

func outputColorEnabled(out *os.File) bool {
	return isInteractiveTerminal(out) && os.Getenv("NO_COLOR") == ""
}

func isInteractiveTerminal(out *os.File) bool {
	if out == nil {
		return false
	}
	info, err := out.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset  = "\x1b[0m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
)

func (p *terminalProgress) UpdateTransfers(task string, state taskprogress.TransferSnapshot) {
	if p == nil || p.out == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if !state.Done && !p.lastTransferReport.IsZero() && now.Sub(p.lastTransferReport) < time.Second {
		return
	}
	p.lastTransferReport = now
	rate := int64(0)
	if state.Elapsed > 0 {
		rate = int64(float64(state.Bytes) / state.Elapsed.Seconds())
	}
	pending := state.Queued - state.Downloaded - state.Failed
	if pending < 0 {
		pending = 0
	}
	line := fmt.Sprintf("%s files: %d downloaded, %d existing, %d failed, %d pending | %s transferred | %s/s | elapsed %s", task, state.Downloaded, state.Existing, state.Failed, pending, formatBytes(state.Bytes), formatBytes(rate), state.Elapsed.Round(time.Second))
	p.transferLine = line
	if p.interactive {
		p.renderLines(p.progressLines())
	} else {
		fmt.Fprintln(p.out, "progress: "+line)
	}
}
