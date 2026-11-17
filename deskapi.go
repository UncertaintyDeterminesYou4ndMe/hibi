package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
)

// The desk API lets a desk device (the ESP-Mosaico board) show today's tasks
// and the timer, send button presses, and report a motion sensor. It is off
// unless settings.json has a "deskApi" block with a listen address and a
// token; every request must carry "Authorization: Bearer <token>".
//
//	GET  /v1/state     the deskState below
//	POST /v1/action    {"action": "...", "task_id": 1, "task_text": "..."}
//	POST /v1/presence  {"motion": true}
//
// Requests run on net/http goroutines. They only call the goroutine-safe
// Store and Pomodoro methods, which notify the menu bar themselves.

// deskAPIPrefs is the "deskApi" block of settings.json.
type deskAPIPrefs struct {
	Listen string `json:"listen,omitempty"` // for example "0.0.0.0:47321"
	Token  string `json:"token,omitempty"`
}

const (
	deskMaxTasks     = 6
	deskMaxBodyBytes = 4 << 10
	// A motion report keeps the user present for this long, so a missed
	// report or two does not end a session.
	deskMotionHold = 2 * time.Minute
)

type deskTask struct {
	ID           uint64 `json:"id"`
	Text         string `json:"text"`
	Quadrant     int    `json:"quadrant"`
	FocusMinutes int    `json:"focus_min"`
}

type deskState struct {
	Phase      string `json:"phase"` // idle | focus | paused | break_pending | break
	RemainingS int    `json:"remaining_s"`
	// SessionS is the full length of the running focus or break, for a
	// progress bar; 0 when idle.
	SessionS         int        `json:"session_s"`
	SelectedFocusMin int        `json:"selected_focus_min"`
	BoundTask        *deskTask  `json:"bound_task"`
	TodayFocusS      int        `json:"today_focus_s"`
	OpenCount        int        `json:"open_count"`
	DoneCount        int        `json:"done_count"`
	Tasks            []deskTask `json:"tasks"`
	Date             string     `json:"date"`
}

type deskActionRequest struct {
	Action   string `json:"action"`
	TaskID   uint64 `json:"task_id,omitempty"`
	TaskText string `json:"task_text,omitempty"`
}

// deskAPI holds what the handlers need, so tests can build one without the
// menu bar app.
type deskAPI struct {
	store        *todo.Store
	timer        *Pomodoro
	token        string
	focusSeconds func(pomodoroSnapshot) int
	// motion records a motion report; it may be nil.
	motion func()
}

func phaseName(phase pomodoroPhase) string {
	switch phase {
	case phaseFocus:
		return "focus"
	case phasePaused:
		return "paused"
	case phaseBreakPending:
		return "break_pending"
	case phaseBreak:
		return "break"
	}
	return "idle"
}

func (d *deskAPI) state() deskState {
	timer := d.timer.Snapshot()
	data := d.store.Snapshot()
	remaining, completed, _ := todo.TodayCounts(data, "")
	s := deskState{
		Phase:            phaseName(timer.Phase),
		RemainingS:       max(0, timer.RemainingSeconds),
		SelectedFocusMin: timer.SelectedFocusMinutes,
		TodayFocusS:      d.focusSeconds(timer),
		OpenCount:        remaining,
		DoneCount:        completed,
		Tasks:            []deskTask{},
		Date:             data.TodayDate,
	}
	switch timer.Phase {
	case phaseFocus, phasePaused:
		s.SessionS = timer.SessionFocusMinutes * 60
	case phaseBreak:
		s.SessionS = breakMinutes * 60
	}
	if timer.BoundTaskText != "" {
		s.BoundTask = &deskTask{ID: timer.BoundTaskID, Text: timer.BoundTaskText}
		// The timer keeps only the ID and text; take the rest from today's list.
		if item, ok := d.findToday(timer.BoundTaskID, timer.BoundTaskText); ok {
			s.BoundTask.Quadrant, s.BoundTask.FocusMinutes = int(item.Quadrant), item.FocusMinutes
		}
	}
	for _, item := range todo.FilteredItems(data, todo.Today, "") {
		if item.Done || len(s.Tasks) == deskMaxTasks {
			continue
		}
		s.Tasks = append(s.Tasks, deskTask{
			ID: item.ID, Text: item.Text,
			Quadrant: int(item.Quadrant), FocusMinutes: item.FocusMinutes,
		})
	}
	return s
}

// findToday resolves a task of today by ID, then by text: IDs change when
// todos.md is edited outside Hibi, and the device may hold an old one.
func (d *deskAPI) findToday(id uint64, text string) (todo.Item, bool) {
	items := d.store.Snapshot().TodayItems
	for _, item := range items {
		if id != 0 && item.ID == id {
			return item, true
		}
	}
	for _, item := range items {
		if text != "" && item.Text == text {
			return item, true
		}
	}
	return todo.Item{}, false
}

var errDeskTaskNotFound = errors.New("task not found")

func (d *deskAPI) act(req deskActionRequest) error {
	switch req.Action {
	case "primary":
		// The device's main button: start, pause, resume, take the break,
		// or skip it, whichever fits the current phase.
		switch d.timer.Snapshot().Phase {
		case phaseIdle:
			if item, ok := d.findToday(req.TaskID, req.TaskText); ok {
				d.timer.StartTask(item, todo.Today, 0)
			} else {
				d.timer.Start(0)
			}
		case phaseFocus:
			d.timer.Pause()
		case phasePaused:
			d.timer.Resume()
		case phaseBreakPending:
			d.timer.StartBreak()
		case phaseBreak:
			d.timer.Skip()
		}
	case "pause":
		d.timer.Pause()
	case "resume":
		d.timer.Resume()
	case "stop":
		d.timer.Stop()
	case "start_break":
		d.timer.StartBreak()
	case "skip":
		d.timer.Skip()
	case "select":
		// Bind the task to the running session, or start one for it.
		item, ok := d.findToday(req.TaskID, req.TaskText)
		if !ok {
			return errDeskTaskNotFound
		}
		d.timer.StartTask(item, todo.Today, 0)
	case "complete":
		item, ok := d.findToday(req.TaskID, req.TaskText)
		if !ok {
			return errDeskTaskNotFound
		}
		// Toggle flips; a repeated press must not reopen the task.
		if !item.Done {
			return d.store.Toggle(item.ID, todo.Today)
		}
	default:
		return errors.New("unknown action")
	}
	return nil
}

func (d *deskAPI) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(d.token)) == 1
}

func writeDeskJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func deskError(w http.ResponseWriter, status int, message string) {
	writeDeskJSON(w, status, map[string]string{"error": message})
}

func (d *deskAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/state", func(w http.ResponseWriter, r *http.Request) {
		writeDeskJSON(w, http.StatusOK, d.state())
	})
	mux.HandleFunc("POST /v1/action", func(w http.ResponseWriter, r *http.Request) {
		var req deskActionRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, deskMaxBodyBytes)).Decode(&req); err != nil {
			deskError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := d.act(req); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errDeskTaskNotFound) {
				status = http.StatusNotFound
			}
			deskError(w, status, err.Error())
			return
		}
		writeDeskJSON(w, http.StatusOK, d.state())
	})
	mux.HandleFunc("POST /v1/presence", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Motion bool `json:"motion"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, deskMaxBodyBytes)).Decode(&req); err != nil {
			deskError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Motion && d.motion != nil {
			d.motion()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !d.authorized(r) {
			deskError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// presenceHold supplements keyboard and mouse idle time with another sign
// that the user is at the computer, as the desk device's motion sensor:
// reading or watching a video without touching the computer no longer
// counts as leaving while the sign keeps coming.
type presenceHold struct {
	last atomic.Int64 // UnixNano of the last report; 0 is none
}

func (t *presenceHold) report() { t.last.Store(time.Now().UnixNano()) }

// idle is the shorter of the system idle time and the time since the last
// report, less the hold period (deskMotionHold).
func (t *presenceHold) idle(system time.Duration) time.Duration {
	last := t.last.Load()
	if last == 0 {
		return system
	}
	since := max(0, time.Since(time.Unix(0, last))-deskMotionHold)
	return min(system, since)
}

// startDeskAPI serves the desk API when settings.json enables it. It returns
// a function that shuts the server down.
func (a *app) startDeskAPI() func() {
	prefs := loadPreferences().DeskAPI
	if prefs == nil || prefs.Listen == "" {
		return func() {}
	}
	if len(prefs.Token) < 16 {
		a.setStatus("桌面设备接口未启动：token 至少 16 个字符")
		return func() {}
	}
	api := &deskAPI{
		store: a.store, timer: a.pomodoro, token: prefs.Token,
		focusSeconds: a.todayFocusSeconds,
		motion: func() {
			a.deskMotion.report()
			a.CheckPresence()
		},
	}
	listener, err := net.Listen("tcp", prefs.Listen)
	if err != nil {
		a.setStatus("桌面设备接口未启动: " + err.Error())
		return func() {}
	}
	server := &http.Server{
		Handler:           api.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("hibi: desk API: %v", err)
		}
	}()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
}
