package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
)

// TestDeskAPIDemoServer serves the desk API on sample tasks for testing a
// desk device without the menu bar app. It runs only when HIBI_DESK_DEMO is
// the listen address and HIBI_DESK_TOKEN the token, and blocks until killed:
//
//	HIBI_DESK_DEMO=0.0.0.0:47321 HIBI_DESK_TOKEN=... go test -run DeskAPIDemo -timeout 0
func TestDeskAPIDemoServer(t *testing.T) {
	listen, token := os.Getenv("HIBI_DESK_DEMO"), os.Getenv("HIBI_DESK_TOKEN")
	if listen == "" || token == "" {
		t.Skip("set HIBI_DESK_DEMO and HIBI_DESK_TOKEN to run the demo server")
	}
	store, err := todo.OpenStore(filepath.Join(t.TempDir(), "todos.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"整理本周的数据质量报告 !1", "回复合作方关于接口变更的邮件", "给新的告警规则写测试用例", "预约体检"} {
		if err := store.Add(task, todo.Today); err != nil {
			t.Fatal(err)
		}
	}
	// One-minute sessions so the end-of-focus chime comes quickly.
	timer := NewPomodoro(store, &fakeTimerHost{}, 1)
	var motion presenceHold
	api := &deskAPI{
		store: store, timer: timer, token: token,
		focusSeconds: func(s pomodoroSnapshot) int { return s.SessionElapsedSeconds },
		motion: func() {
			motion.report()
			t.Log("motion reported")
		},
	}
	t.Logf("desk API demo on %s", listen)
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		api.handler().ServeHTTP(w, r)
	})
	if err := http.ListenAndServe(listen, logged); err != nil {
		t.Fatal(err)
	}
}
