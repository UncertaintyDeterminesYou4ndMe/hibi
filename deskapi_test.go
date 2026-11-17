package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
)

const testDeskToken = "test-token-0123456789"

func newTestDeskAPI(t *testing.T, tasks ...string) (*deskAPI, *todo.Store, *Pomodoro, *int) {
	t.Helper()
	store, err := todo.OpenStore(filepath.Join(t.TempDir(), "todos.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if err := store.Add(task, todo.Today); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPomodoro(store, &fakeTimerHost{}, 25)
	t.Cleanup(p.Stop)
	motions := 0
	api := &deskAPI{
		store: store, timer: p, token: testDeskToken,
		focusSeconds: func(s pomodoroSnapshot) int { return 600 + s.SessionElapsedSeconds },
		motion:       func() { motions++ },
	}
	return api, store, p, &motions
}

func deskRequest(t *testing.T, api *deskAPI, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	api.handler().ServeHTTP(rec, req)
	return rec
}

func decodeDeskState(t *testing.T, rec *httptest.ResponseRecorder) deskState {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var s deskState
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDeskAPIRejectsMissingOrWrongToken(t *testing.T) {
	api, _, _, _ := newTestDeskAPI(t)
	for _, token := range []string{"", "wrong-token-0123456789"} {
		if rec := deskRequest(t, api, "GET", "/v1/state", "", token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: status=%d", token, rec.Code)
		}
	}
}

func TestDeskAPIStateListsOpenTodayTasks(t *testing.T) {
	api, store, _, _ := newTestDeskAPI(t, "写报告 !1", "回邮件", "已完成的事")
	done := store.Snapshot().TodayItems[2]
	if err := store.Toggle(done.ID, todo.Today); err != nil {
		t.Fatal(err)
	}
	s := decodeDeskState(t, deskRequest(t, api, "GET", "/v1/state", "", testDeskToken))
	if s.Phase != "idle" || s.SessionS != 0 || s.SelectedFocusMin != 25 || s.TodayFocusS != 600 {
		t.Fatalf("state=%#v", s)
	}
	if s.OpenCount != 2 || s.DoneCount != 1 || len(s.Tasks) != 2 {
		t.Fatalf("counts=%d/%d tasks=%#v", s.OpenCount, s.DoneCount, s.Tasks)
	}
	if s.Tasks[0].Text != "写报告" || s.Tasks[0].Quadrant != 1 || s.Tasks[1].Text != "回邮件" {
		t.Fatalf("tasks=%#v", s.Tasks)
	}
}

func TestDeskAPIPrimaryWalksThroughThePhases(t *testing.T) {
	api, store, p, _ := newTestDeskAPI(t, "写报告")
	item := store.Snapshot().TodayItems[0]
	start := `{"action":"primary","task_id":` + jsonNumber(item.ID) + `}`
	s := decodeDeskState(t, deskRequest(t, api, "POST", "/v1/action", start, testDeskToken))
	if s.Phase != "focus" || s.SessionS != 25*60 || s.BoundTask == nil || s.BoundTask.Text != "写报告" {
		t.Fatalf("after start=%#v", s)
	}
	primary := `{"action":"primary"}`
	if s := decodeDeskState(t, deskRequest(t, api, "POST", "/v1/action", primary, testDeskToken)); s.Phase != "paused" {
		t.Fatalf("after pause=%#v", s)
	}
	if s := decodeDeskState(t, deskRequest(t, api, "POST", "/v1/action", primary, testDeskToken)); s.Phase != "focus" {
		t.Fatalf("after resume=%#v", s)
	}
	finishCountdown(p)
	if s := decodeDeskState(t, deskRequest(t, api, "GET", "/v1/state", "", testDeskToken)); s.Phase != "break_pending" {
		t.Fatalf("after focus=%#v", s)
	}
	s = decodeDeskState(t, deskRequest(t, api, "POST", "/v1/action", primary, testDeskToken))
	if s.Phase != "break" || s.SessionS != breakMinutes*60 {
		t.Fatalf("after break start=%#v", s)
	}
	if s := decodeDeskState(t, deskRequest(t, api, "POST", "/v1/action", primary, testDeskToken)); s.Phase != "focus" {
		t.Fatalf("after skip=%#v", s)
	}
}

func TestDeskAPICompleteIsIdempotentAndFallsBackToText(t *testing.T) {
	api, store, _, _ := newTestDeskAPI(t, "写报告", "回邮件")
	// An ID from before an external edit no longer exists; the text still matches.
	body := `{"action":"complete","task_id":999999,"task_text":"回邮件"}`
	for range 2 {
		s := decodeDeskState(t, deskRequest(t, api, "POST", "/v1/action", body, testDeskToken))
		if s.OpenCount != 1 || s.DoneCount != 1 {
			t.Fatalf("state=%#v", s)
		}
	}
	for _, item := range store.Snapshot().TodayItems {
		if item.Text == "回邮件" && !item.Done {
			t.Fatal("task should stay done after a repeated press")
		}
	}
	missing := `{"action":"complete","task_text":"没有这件事"}`
	if rec := deskRequest(t, api, "POST", "/v1/action", missing, testDeskToken); rec.Code != http.StatusNotFound {
		t.Fatalf("missing task status=%d", rec.Code)
	}
}

func TestDeskAPISelectBindsTheRunningSession(t *testing.T) {
	api, store, p, _ := newTestDeskAPI(t, "写报告", "回邮件")
	p.Start(25)
	second := store.Snapshot().TodayItems[1]
	body := `{"action":"select","task_id":` + jsonNumber(second.ID) + `}`
	s := decodeDeskState(t, deskRequest(t, api, "POST", "/v1/action", body, testDeskToken))
	if s.Phase != "focus" || s.BoundTask == nil || s.BoundTask.Text != "回邮件" {
		t.Fatalf("state=%#v", s)
	}
}

func TestDeskAPIRejectsUnknownActionAndBadJSON(t *testing.T) {
	api, _, _, _ := newTestDeskAPI(t)
	if rec := deskRequest(t, api, "POST", "/v1/action", `{"action":"explode"}`, testDeskToken); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action status=%d", rec.Code)
	}
	if rec := deskRequest(t, api, "POST", "/v1/action", `{`, testDeskToken); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status=%d", rec.Code)
	}
}

func TestDeskAPIPresenceReportsMotion(t *testing.T) {
	api, _, _, motions := newTestDeskAPI(t)
	if rec := deskRequest(t, api, "POST", "/v1/presence", `{"motion":true}`, testDeskToken); rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d", rec.Code)
	}
	deskRequest(t, api, "POST", "/v1/presence", `{"motion":false}`, testDeskToken)
	if *motions != 1 {
		t.Fatalf("motions=%d", *motions)
	}
}

func TestDeskMotionTrackerShortensIdleTime(t *testing.T) {
	var tracker presenceHold
	if got := tracker.idle(10 * time.Minute); got != 10*time.Minute {
		t.Fatalf("without reports idle=%v", got)
	}
	tracker.report()
	if got := tracker.idle(10 * time.Minute); got != 0 {
		t.Fatalf("right after a report idle=%v", got)
	}
	// A report older than the hold period counts from the end of the hold.
	tracker.last.Store(time.Now().Add(-deskMotionHold - 3*time.Minute).UnixNano())
	if got := tracker.idle(10 * time.Minute); got < 3*time.Minute || got > 3*time.Minute+time.Second {
		t.Fatalf("old report idle=%v", got)
	}
	if got := tracker.idle(time.Minute); got != time.Minute {
		t.Fatalf("recent keyboard input should win, idle=%v", got)
	}
}

func jsonNumber(id uint64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
