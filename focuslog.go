package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// focusLog keeps the focus seconds of each day, sessions with no task
// included, so the panel can show today's total. It lives next to
// settings.json, not in todos.md: it is the timer's record, not a task.
type focusLog struct {
	mu   sync.Mutex
	path string
	days map[string]int // local date "2006-01-02" → seconds
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// openFocusLog reads the log at path. A missing or unreadable file starts
// an empty log rather than failing the app.
func openFocusLog(path string) *focusLog {
	l := &focusLog{path: path, days: map[string]int{}}
	if contents, err := os.ReadFile(path); err == nil {
		var file struct {
			Days map[string]int `json:"days"`
		}
		if json.Unmarshal(contents, &file) == nil && file.Days != nil {
			l.days = file.Days
		}
	}
	return l
}

// Seconds returns the focus seconds recorded for day. A nil log has none.
func (l *focusLog) Seconds(day string) int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.days[day]
}

// Add records seconds of focus on day and writes the log.
func (l *focusLog) Add(day string, seconds int) error {
	if l == nil || seconds <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.days[day] += seconds
	contents, err := json.MarshalIndent(struct {
		Days map[string]int `json:"days"`
	}{l.days}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, append(contents, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// formatFocus shows a duration of focus as "45m" or "2h05m".
func formatFocus(seconds int) string {
	minutes := max(0, seconds) / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
}
