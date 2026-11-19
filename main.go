package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
	"github.com/egoist/mygo"
)

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--selftest" {
			if !runSelfTest() {
				os.Exit(1)
			}
			return
		}
	}

	mygo.App.SetName("Hibi")
	// One Hibi at a time: a second would show a second icon, time a second
	// session and find the desk API's port taken. Opened again, Hibi hands
	// over to the one that runs, which opens its panel (OnSecondInstance).
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	store, err := todo.OpenStore(filepath.Join(home, "todos.md"))
	if err != nil {
		log.Fatal(err)
	}
	a := newApp(store)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnSecondInstance(func([]string, string) { a.openPanel() })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting.Store(true) })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.deskAPIStop != nil {
			a.deskAPIStop()
		}
		a.stopPresence()
		a.pomodoro.Stop()
		if a.watchCancel != nil {
			a.watchCancel()
		}
		if tray := a.currentTray(); tray != nil {
			tray.Destroy()
		}
	})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func runSelfTest() (ok bool) {
	ok = true
	check := func(name string, condition bool) {
		if condition {
			fmt.Println("PASS:", name)
		} else {
			fmt.Println("FAIL:", name)
			ok = false
		}
	}
	input := "# Todos\n\n## 今日 2026-07-17\n\n- [ ] 写报告 #work !1 ⏱40m\n\n## 三日\n\n## 本周 2026-W29\n\n- [ ] 周任务\n\n## 归档\n"
	parsed := todo.Parse(input)
	check("Markdown parsing", len(parsed.TodayItems) == 1 && parsed.TodayItems[0].Text == "写报告" && parsed.TodayItems[0].FocusMinutes == 40)
	check("legacy week migration", len(parsed.WeekItems) == 1 && !strings.Contains(todo.Serialize(parsed), "2026-W29"))
	check("canonical round-trip", todo.Parse(todo.Serialize(parsed)).SameContent(parsed))
	check("focus settlement", todo.MinutesToCredit(125, false, 40) == 2 && todo.MinutesToCredit(0, true, 40) == 40)
	if ok {
		fmt.Println("ALL PASS")
	}
	return ok
}
