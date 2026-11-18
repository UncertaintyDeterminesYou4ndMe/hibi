package main

import (
	"bytes"
	"image/png"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
)

type fakeTimerHost struct {
	mu             sync.Mutex
	changes        int
	prompts        int
	hides          int
	chimes         int
	fronts         int
	notifications  []string
	savedMinutes   int
	focusSeconds   int
	presenceChecks int
}

func (f *fakeTimerHost) CheckPresence() { f.mu.Lock(); f.presenceChecks++; f.mu.Unlock() }

func (f *fakeTimerHost) RecordFocus(seconds int) {
	f.mu.Lock()
	f.focusSeconds += seconds
	f.mu.Unlock()
}

func (f *fakeTimerHost) TimerChanged()            { f.mu.Lock(); f.changes++; f.mu.Unlock() }
func (f *fakeTimerHost) ShowBreakPrompt(int, int) { f.mu.Lock(); f.prompts++; f.mu.Unlock() }
func (f *fakeTimerHost) HideBreakPrompt()         { f.mu.Lock(); f.hides++; f.mu.Unlock() }
func (f *fakeTimerHost) BringBreakPromptToFront() { f.mu.Lock(); f.fronts++; f.mu.Unlock() }
func (f *fakeTimerHost) Chime()                   { f.mu.Lock(); f.chimes++; f.mu.Unlock() }
func (f *fakeTimerHost) Notify(title, body string) {
	f.mu.Lock()
	f.notifications = append(f.notifications, title+":"+body)
	f.mu.Unlock()
}
func (f *fakeTimerHost) SaveFocusMinutes(minutes int) {
	f.mu.Lock()
	f.savedMinutes = minutes
	f.mu.Unlock()
}

func TestPomodoroNaturalCompletionAndBreak(t *testing.T) {
	store, err := todo.OpenStore(filepath.Join(t.TempDir(), "todos.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("专注任务", todo.Today); err != nil {
		t.Fatal(err)
	}
	item := store.Snapshot().TodayItems[0]
	host := &fakeTimerHost{}
	p := NewPomodoro(store, host, 40)
	p.StartTask(item, todo.Today, 5)
	if got := p.Snapshot(); got.Phase != phaseFocus || got.RemainingSeconds != 300 || got.BoundTaskID != item.ID {
		t.Fatalf("start snapshot=%#v", got)
	}
	finishCountdown(p)
	if got := p.Snapshot(); got.Phase != phaseBreakPending || got.RemainingSeconds != 0 || got.BoundTaskID != 0 {
		t.Fatalf("focus completion snapshot=%#v", got)
	}
	if got := store.Snapshot().TodayItems[0].FocusMinutes; got != 5 {
		t.Fatalf("credited minutes=%d", got)
	}
	if host.prompts != 1 || host.chimes != 1 || host.savedMinutes != 5 {
		t.Fatalf("host=%#v", host)
	}

	p.StartBreak()
	if got := p.Snapshot(); got.Phase != phaseBreak || got.RemainingSeconds != 600 {
		t.Fatalf("break start=%#v", got)
	}
	finishCountdown(p)
	if got := p.Snapshot(); got.Phase != phaseIdle || got.RemainingSeconds != 0 {
		t.Fatalf("break completion=%#v", got)
	}
	if len(host.notifications) != 1 || host.notifications[0] != "休息结束:你在电脑前时，下一轮专注会自动开始" {
		t.Fatalf("notifications=%v", host.notifications)
	}
}

func TestPomodoroEarlyStopAndReplacement(t *testing.T) {
	store, err := todo.OpenStore(filepath.Join(t.TempDir(), "todos.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("任务", todo.Today); err != nil {
		t.Fatal(err)
	}
	item := store.Snapshot().TodayItems[0]
	p := NewPomodoro(store, nil, 40)
	p.StartTask(item, todo.Today, 15)
	p.Pause()
	setRemaining(p, 15*60-125)
	p.Stop()
	if got := store.Snapshot().TodayItems[0].FocusMinutes; got != 2 {
		t.Fatalf("early-stop credit=%d", got)
	}
	p.Start(30)
	p.Start(5) // replacing a free session has no task credit
	if got := p.Snapshot(); got.Phase != phaseFocus || got.SelectedFocusMinutes != 5 || got.SessionFocusMinutes != 5 {
		t.Fatalf("replacement=%#v", got)
	}
	p.Stop()
}

func TestBreakReminderLimit(t *testing.T) {
	host := &fakeTimerHost{}
	p := NewPomodoro(nil, host, 40)
	p.phase = phaseBreakPending
	p.startReminderLocked(notBusy, 0)
	p.mu.Lock()
	generation := p.reminderGeneration
	p.mu.Unlock()
	for range breakReminderLimit + 1 {
		p.remind(generation)
	}
	if host.chimes != breakReminderLimit || host.fronts != breakReminderLimit {
		t.Fatalf("chimes=%d fronts=%d", host.chimes, host.fronts)
	}
	p.Stop()
}

func newTaskStore(t *testing.T, texts ...string) (*todo.Store, map[string]todo.Item) {
	t.Helper()
	store, err := todo.OpenStore(filepath.Join(t.TempDir(), "todos.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range texts {
		if err := store.Add(text, todo.Today); err != nil {
			t.Fatal(err)
		}
	}
	items := map[string]todo.Item{}
	for _, item := range store.Snapshot().TodayItems {
		items[item.Text] = item
	}
	return store, items
}

func creditedMinutes(t *testing.T, store *todo.Store, text string) int {
	t.Helper()
	for _, item := range store.Snapshot().TodayItems {
		if item.Text == text {
			return item.FocusMinutes
		}
	}
	t.Fatalf("task %q not found", text)
	return 0
}

func setRemaining(p *Pomodoro, seconds int) {
	p.mu.Lock()
	left := time.Duration(seconds) * time.Second
	if p.runningLocked() {
		p.deadline = p.clock() + left
		p.scheduleLocked()
	} else {
		p.left = left
	}
	p.mu.Unlock()
}

// finishCountdown brings the running focus or break to its end.
func finishCountdown(p *Pomodoro) {
	p.mu.Lock()
	p.deadline = p.clock()
	generation := p.timerGeneration
	p.mu.Unlock()
	p.wake(generation)
}

func TestAutoStartFollowsPresence(t *testing.T) {
	host := &fakeTimerHost{}
	p := NewPomodoro(nil, host, 15)
	defer p.Stop()

	p.UserPresent() // armed at launch
	if got := p.Snapshot(); got.Phase != phaseFocus || got.RemainingSeconds != 15*60 || got.BoundTaskID != 0 {
		t.Fatalf("auto-start at launch=%#v", got)
	}
	if host.savedMinutes != 0 {
		t.Fatalf("auto-start rewrote the remembered length: %d", host.savedMinutes)
	}
	p.UserPresent() // a running session is left alone
	if got := p.Snapshot(); got.RemainingSeconds != 15*60 {
		t.Fatalf("second present restarted the session: %#v", got)
	}

	p.Stop()
	p.UserPresent()
	if got := p.Snapshot(); got.Phase != phaseIdle {
		t.Fatalf("auto-start after a manual stop: %#v", got)
	}
	p.UserAway(defaultAwayThreshold)
	p.UserPresent()
	if got := p.Snapshot(); got.Phase != phaseFocus {
		t.Fatalf("no auto-start after returning: %#v", got)
	}
}

func TestUserAwayEndsFocusAndCreditsTimeBeforeLeaving(t *testing.T) {
	store, items := newTaskStore(t, "写报告")
	host := &fakeTimerHost{}
	p := NewPomodoro(store, host, 40)
	defer p.Stop()
	p.StartTask(items["写报告"], todo.Today, 40)
	setRemaining(p, 20*60) // 20 minutes counted, the last 6 of them without input
	p.UserAway(6 * time.Minute)
	if got := p.Snapshot(); got.Phase != phaseIdle || got.BoundTaskID != 0 {
		t.Fatalf("after leaving=%#v", got)
	}
	if got := creditedMinutes(t, store, "写报告"); got != 14 {
		t.Fatalf("credited=%d, want 14", got)
	}

	p.StartTask(items["写报告"], todo.Today, 40)
	p.Pause()
	setRemaining(p, 37*60) // paused after 3 minutes; the countdown stood still while away
	p.UserAway(30 * time.Minute)
	if got := creditedMinutes(t, store, "写报告"); got != 17 {
		t.Fatalf("credited after paused leave=%d, want 17", got)
	}
	p.UserPresent()
	if got := p.Snapshot(); got.Phase != phaseFocus || got.BoundTaskID != 0 {
		t.Fatalf("return after leaving=%#v", got)
	}
}

func TestAutoSenseSettingsTurnTheSignalsOff(t *testing.T) {
	store, items := newTaskStore(t, "写报告")
	host := &fakeTimerHost{}
	p := NewPomodoro(store, host, 40)
	defer p.Stop()

	p.SetAutoSense(autoSense{AutoStart: false, EndOnAway: true, AwayMinutes: 5})
	p.UserPresent()
	if got := p.Snapshot(); got.Phase != phaseIdle {
		t.Fatalf("auto-start off, yet present started a session: %#v", got)
	}

	p.SetAutoSense(autoSense{AutoStart: true, EndOnAway: false, AwayMinutes: 5})
	p.StartTask(items["写报告"], todo.Today, 40)
	setRemaining(p, 20*60)
	p.UserAway(6 * time.Minute)
	if got := p.Snapshot(); got.Phase != phaseFocus || got.BoundTaskID == 0 {
		t.Fatalf("end-on-away off, yet leaving ended the session: %#v", got)
	}
	// Leaving still arms the start for the return.
	p.Stop()
	p.UserAway(6 * time.Minute)
	p.UserPresent()
	if got := p.Snapshot(); got.Phase != phaseFocus {
		t.Fatalf("no auto-start after returning with end-on-away off: %#v", got)
	}

	// With auto-start off, the break-end notice does not promise one.
	p.SetAutoSense(autoSense{AutoStart: false, EndOnAway: true, AwayMinutes: 5})
	finishCountdown(p)
	p.StartBreak()
	finishCountdown(p)
	host.mu.Lock()
	last := host.notifications[len(host.notifications)-1]
	host.mu.Unlock()
	if last != "休息结束:可以开始下一轮专注" {
		t.Fatalf("break-end notice=%q", last)
	}
	p.UserPresent()
	if got := p.Snapshot(); got.Phase != phaseIdle {
		t.Fatalf("auto-start off, yet the break end started a session: %#v", got)
	}
}

func TestUserAwayClosesBreakPromptButKeepsRunningBreak(t *testing.T) {
	host := &fakeTimerHost{}
	p := NewPomodoro(nil, host, 5)
	defer p.Stop()
	p.Start(5)
	finishCountdown(p)
	if got := p.Snapshot(); got.Phase != phaseBreakPending || host.prompts != 1 {
		t.Fatalf("focus end=%#v prompts=%d", got, host.prompts)
	}
	hides := host.hides
	p.UserAway(defaultAwayThreshold)
	if got := p.Snapshot(); got.Phase != phaseIdle || host.hides != hides+1 || p.reminderCancel != nil {
		t.Fatalf("leaving at the prompt=%#v hides=%d", got, host.hides)
	}

	p.UserPresent()
	finishCountdown(p)
	p.StartBreak()
	setRemaining(p, 4*60)
	p.UserAway(defaultAwayThreshold)
	if got := p.Snapshot(); got.Phase != phaseBreak || got.RemainingSeconds != 4*60 {
		t.Fatalf("leaving during the break=%#v", got)
	}
}

func TestBreakEndAndSkipLeadIntoTheNextFocus(t *testing.T) {
	host := &fakeTimerHost{}
	p := NewPomodoro(nil, host, 5)
	defer p.Stop()
	p.Start(5) // a manual start; the next one must still come by itself
	finishCountdown(p)
	p.StartBreak()
	finishCountdown(p)
	if got := p.Snapshot(); got.Phase != phaseIdle || host.presenceChecks != 1 {
		t.Fatalf("break end=%#v presenceChecks=%d", got, host.presenceChecks)
	}
	p.UserPresent()
	if got := p.Snapshot(); got.Phase != phaseFocus || got.RemainingSeconds != 5*60 {
		t.Fatalf("after break=%#v", got)
	}

	finishCountdown(p)
	p.Skip()
	if got := p.Snapshot(); got.Phase != phaseFocus || got.RemainingSeconds != 5*60 {
		t.Fatalf("skip at the prompt=%#v", got)
	}
	finishCountdown(p)
	p.StartBreak()
	p.Skip()
	if got := p.Snapshot(); got.Phase != phaseFocus || got.RemainingSeconds != 5*60 {
		t.Fatalf("skip during the break=%#v", got)
	}
}

func TestPlayBindsTaskToRunningSession(t *testing.T) {
	store, items := newTaskStore(t, "A", "B")
	p := NewPomodoro(store, &fakeTimerHost{}, 40)
	defer p.Stop()
	p.UserPresent()
	setRemaining(p, 30*60) // ten minutes in, no task

	p.StartTask(items["A"], todo.Today, 0)
	if got := p.Snapshot(); got.Phase != phaseFocus || got.RemainingSeconds != 30*60 || got.BoundTaskText != "A" || got.SessionFocusMinutes != 40 {
		t.Fatalf("bind A=%#v", got)
	}
	setRemaining(p, 22*60+30) // 7.5 minutes on A
	p.StartTask(items["B"], todo.Today, 0)
	if got := p.Snapshot(); got.RemainingSeconds != 22*60+30 || got.BoundTaskText != "B" {
		t.Fatalf("switch to B=%#v", got)
	}
	if got := creditedMinutes(t, store, "A"); got != 7 {
		t.Fatalf("A credited=%d, want 7", got)
	}

	setRemaining(p, 19*60+30) // 3 minutes on B
	p.Pause()
	p.StartTask(items["A"], todo.Today, 0) // play while paused also resumes
	if got := p.Snapshot(); got.Phase != phaseFocus || got.BoundTaskText != "A" || got.RemainingSeconds != 19*60+30 {
		t.Fatalf("bind A while paused=%#v", got)
	}
	if got := creditedMinutes(t, store, "B"); got != 3 {
		t.Fatalf("B credited=%d, want 3", got)
	}
	finishCountdown(p) // A was bound for the last 19.5 minutes
	if got := creditedMinutes(t, store, "A"); got != 7+19 {
		t.Fatalf("A credited after completion=%d, want 26", got)
	}

	p.Skip()
	p.StartTask(items["B"], todo.Today, 15) // an explicit length starts over
	if got := p.Snapshot(); got.RemainingSeconds != 15*60 || got.SessionFocusMinutes != 15 || got.BoundTaskText != "B" {
		t.Fatalf("explicit length=%#v", got)
	}
}

func TestFocusSecondsReachTheDayTotal(t *testing.T) {
	store, items := newTaskStore(t, "A")
	host := &fakeTimerHost{}
	p := NewPomodoro(store, host, 40)
	defer p.Stop()

	p.UserPresent()
	setRemaining(p, 30*60)
	if got := p.Snapshot().SessionElapsedSeconds; got != 10*60 {
		t.Fatalf("live elapsed=%d", got)
	}
	p.StartTask(items["A"], todo.Today, 0) // a task switch is not an ending
	if host.focusSeconds != 0 {
		t.Fatalf("switch recorded %d", host.focusSeconds)
	}
	setRemaining(p, 20*60)
	p.UserAway(6 * time.Minute) // 20 minutes counted, 6 of them away
	if host.focusSeconds != 14*60 {
		t.Fatalf("after leaving=%d, want %d", host.focusSeconds, 14*60)
	}

	p.UserPresent()
	finishCountdown(p) // a full session
	if host.focusSeconds != 14*60+40*60 {
		t.Fatalf("after completion=%d", host.focusSeconds)
	}
	p.StartBreak()
	p.Skip()
	setRemaining(p, 40*60-90)
	p.Stop()
	if host.focusSeconds != 14*60+40*60+90 {
		t.Fatalf("after stop=%d", host.focusSeconds)
	}
}

func TestFocusLogPersistsAndFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Hibi", "focus.json")
	log := openFocusLog(path)
	if err := log.Add("2026-11-14", 90); err != nil {
		t.Fatal(err)
	}
	if err := log.Add("2026-11-14", 3600); err != nil {
		t.Fatal(err)
	}
	if got := openFocusLog(path).Seconds("2026-11-14"); got != 3690 {
		t.Fatalf("reopened=%d", got)
	}
	var none *focusLog
	if none.Seconds("2026-11-14") != 0 || none.Add("2026-11-14", 5) != nil {
		t.Fatal("nil log")
	}
	for seconds, want := range map[int]string{0: "0m", 59: "0m", 45 * 60: "45m", 125 * 60: "2h05m", -5: "0m"} {
		if got := formatFocus(seconds); got != want {
			t.Fatalf("formatFocus(%d)=%q want %q", seconds, got, want)
		}
	}
}

func TestTrayTitleShowsNoTimeLeft(t *testing.T) {
	for _, tc := range []struct {
		snapshot pomodoroSnapshot
		open     int
		want     string
	}{
		{pomodoroSnapshot{Phase: phaseFocus, RemainingSeconds: 40 * 60}, 3, ""},
		{pomodoroSnapshot{Phase: phaseFocus, RemainingSeconds: 1}, 3, ""},
		{pomodoroSnapshot{Phase: phasePaused, RemainingSeconds: 90}, 3, "暂停"},
		{pomodoroSnapshot{Phase: phaseBreak, RemainingSeconds: 600}, 3, "休息"},
		{pomodoroSnapshot{Phase: phaseBreakPending}, 3, "该休息了"},
		{pomodoroSnapshot{Phase: phaseIdle}, 3, "3"},
		{pomodoroSnapshot{Phase: phaseIdle}, 0, ""},
	} {
		if got := trayTitle(tc.snapshot, tc.open); got != tc.want {
			t.Fatalf("trayTitle(%v, %d)=%q want %q", tc.snapshot.Phase, tc.open, got, tc.want)
		}
	}
}

// Each mood has its own menu bar icon, 16 points at 2x.
func TestTrayCatHasAnIconForEachMood(t *testing.T) {
	seen := map[string]catMood{}
	for mood := moodAwake; mood <= moodAsleep; mood++ {
		icon := trayIcon(mood)
		if other, ok := seen[string(icon)]; ok {
			t.Fatalf("moods %d and %d share an icon", other, mood)
		}
		seen[string(icon)] = mood
		img, err := png.Decode(bytes.NewReader(icon))
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
			t.Fatalf("mood %d: icon is %v, want 32x32", mood, b)
		}
	}
}

// The cat tires with the day's focus and with sitting, whichever is more
// tiring, and sleeps through a break.
func TestCatMood(t *testing.T) {
	h := time.Hour
	half := sitting{sitEvery: 30 * time.Minute}
	sat := func(periods int) sitting { s := half; s.periods = periods; return s }
	for _, tc := range []struct {
		phase pomodoroPhase
		today time.Duration
		sat   sitting
		mood  catMood
		tip   string
	}{
		{phaseFocus, 0, sitting{}, moodAwake, ""},
		{phaseIdle, 3*h + 59*time.Minute, sat(0), moodAwake, ""},
		{phaseFocus, 4 * h, sat(0), moodTired, "有点累了：今天已经专注 4 小时"},
		{phaseFocus, 6*h + time.Minute, sat(0), moodWeary, "很累了：今天已经专注 6 小时"},
		{phaseFocus, 9 * h, sat(0), moodExhausted, "累坏了：今天已经专注 8 小时"},
		{phaseFocus, h, sat(1), moodTired, "有点累了：已经连续坐了 30 分钟"},
		{phasePaused, h, sat(2), moodWeary, "很累了：已经连续坐了 60 分钟"},
		{phaseIdle, h, sat(5), moodExhausted, "累坏了：已经连续坐了 150 分钟"},
		// Both reach the mood: both say why.
		{phaseFocus, 4 * h, sat(1), moodTired, "有点累了：今天已经专注 4 小时，已经连续坐了 30 分钟"},
		// The more tiring one decides.
		{phaseFocus, 6 * h, sat(1), moodWeary, "很累了：今天已经专注 6 小时"},
		// The sitting reminder is off: sitting does not count.
		{phaseFocus, h, sitting{periods: 3}, moodAwake, ""},
		// A break: asleep, however tired.
		{phaseBreak, 9 * h, sat(4), moodAsleep, "休息中"},
		{phaseBreakPending, 4 * h, sat(0), moodTired, "有点累了：今天已经专注 4 小时"},
	} {
		mood, tip := catMoodFor(tc.phase, tc.today, tc.sat)
		if mood != tc.mood || tip != tc.tip {
			t.Errorf("catMoodFor(%v, %v, %+v) = %d %q, want %d %q", tc.phase, tc.today, tc.sat, mood, tip, tc.mood, tc.tip)
		}
	}
}

func TestSavingFocusMinutesKeepsTheHotkey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := savePreferences(preferences{Hotkey: "Ctrl+Alt+J"}); err != nil {
		t.Fatal(err)
	}
	(&app{}).SaveFocusMinutes(15)
	prefs := loadPreferences()
	if prefs.Hotkey != "Ctrl+Alt+J" || prefs.focusMinutes() != 15 {
		t.Fatalf("prefs=%#v", prefs)
	}
	if (preferences{FocusMinutes: 7}).focusMinutes() != 40 {
		t.Fatal("unknown length should fall back to 40")
	}
}

// fakeClock drives a Pomodoro's monotonic clock by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Duration
}

func (c *fakeClock) read() time.Duration { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	c.mu.Unlock()
}

func TestTimerWakesOnlyWhenTheDisplayChanges(t *testing.T) {
	clock := &fakeClock{}
	p := NewPomodoro(nil, &fakeTimerHost{}, 40)
	p.clock = clock.read
	defer p.Stop()
	waits := func() time.Duration {
		p.mu.Lock()
		defer p.mu.Unlock()
		left := p.leftLocked()
		p.scheduleLocked()
		step := time.Minute
		if p.fineTicks {
			step = time.Second
		}
		wait := left % step
		if wait == 0 {
			wait = min(step, left)
		}
		return wait
	}

	p.Start(40)
	if got := waits(); got != time.Minute {
		t.Fatalf("coarse wait at 40:00 = %v", got)
	}
	clock.advance(25 * time.Second) // 39:35 left: the menu bar shows 40m until 39:00
	if got := waits(); got != 35*time.Second {
		t.Fatalf("coarse wait at 39:35 = %v", got)
	}
	if got := p.Snapshot().RemainingSeconds; got != 39*60+35 {
		t.Fatalf("remaining=%d", got)
	}
	p.SetFineTicks(true)
	clock.advance(300 * time.Millisecond)
	if got := waits(); got != 700*time.Millisecond {
		t.Fatalf("fine wait = %v", got)
	}
	if got := p.Snapshot().RemainingSeconds; got != 39*60+35 {
		t.Fatalf("remaining rounds up: %d", got)
	}

	// Paused, the clock runs on and the countdown does not.
	p.Pause()
	clock.advance(time.Hour)
	p.Resume()
	if got := p.Snapshot().RemainingSeconds; got != 39*60+35 {
		t.Fatalf("after a paused hour remaining=%d", got)
	}
	clock.advance(39*time.Minute + 35*time.Second)
	if got := p.Snapshot().RemainingSeconds; got != 0 {
		t.Fatalf("at the end remaining=%d", got)
	}
}

func TestTimerRunsOnRealTime(t *testing.T) {
	host := &fakeTimerHost{}
	p := NewPomodoro(nil, host, 40)
	defer p.Stop()
	p.Start(40)
	p.SetFineTicks(true)
	setRemaining(p, 2)
	// The phase changes before the prompt shows, outside the lock: wait
	// for both.
	prompted := func() bool {
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.prompts > 0
	}
	deadline := time.Now().Add(5 * time.Second)
	for p.Snapshot().Phase != phaseBreakPending || !prompted() {
		if time.Now().After(deadline) {
			t.Fatalf("countdown did not end with a prompt: %#v", p.Snapshot())
		}
		time.Sleep(20 * time.Millisecond)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.prompts != 1 || host.changes < 3 {
		t.Fatalf("prompts=%d changes=%d", host.prompts, host.changes)
	}
}

// busyScript is a busy reader that says what it is told, and counts the
// times it was asked.
type busyScript struct {
	mu     sync.Mutex
	reason busyReason
	asked  int
}

func (b *busyScript) read() busyReason {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.asked++
	return b.reason
}

func (b *busyScript) set(r busyReason) { b.mu.Lock(); b.reason = r; b.mu.Unlock() }

func (f *fakeTimerHost) promptCount() (prompts, chimes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prompts, f.chimes
}

// waitFor polls cond for up to two seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFocusEndPromptWaitsForAPauseInTyping(t *testing.T) {
	host := &fakeTimerHost{}
	busy := &busyScript{reason: busyTyping}
	p := NewPomodoro(nil, host, 5)
	p.busy, p.deferCheck = busy.read, time.Millisecond
	defer p.Stop()
	p.Start(5)
	finishCountdown(p)
	if got := p.Snapshot().Phase; got != phaseBreakPending {
		t.Fatalf("phase=%v", got)
	}
	if prompts, chimes := host.promptCount(); prompts != 0 || chimes != 0 {
		t.Fatalf("prompted while typing: prompts=%d chimes=%d", prompts, chimes)
	}
	waitFor(t, "a few checks", func() bool { busy.mu.Lock(); defer busy.mu.Unlock(); return busy.asked > 3 })
	busy.set(notBusy)
	waitFor(t, "the prompt after the pause", func() bool { prompts, _ := host.promptCount(); return prompts == 1 })
	if _, chimes := host.promptCount(); chimes != 1 {
		t.Fatalf("chimes=%d", chimes)
	}
}

func TestFocusEndPromptWaitsNoLongerThanTheLimit(t *testing.T) {
	clock := &fakeClock{}
	host := &fakeTimerHost{}
	busy := &busyScript{reason: busyTyping}
	p := NewPomodoro(nil, host, 5)
	p.clock, p.busy, p.deferCheck = clock.read, busy.read, time.Millisecond
	defer p.Stop()
	p.Start(5)
	finishCountdown(p)
	busy.set(busyCall) // a call began: the call's limit holds now
	clock.advance(typingDeferLimit)
	time.Sleep(20 * time.Millisecond)
	if prompts, _ := host.promptCount(); prompts != 0 {
		t.Fatal("the typing limit cut a call short")
	}
	clock.advance(callDeferLimit)
	waitFor(t, "the prompt at the limit", func() bool { prompts, _ := host.promptCount(); return prompts == 1 })
}

func TestDeferredPromptDoesNotComeAfterTheUserLeft(t *testing.T) {
	host := &fakeTimerHost{}
	busy := &busyScript{reason: busyCall}
	p := NewPomodoro(nil, host, 5)
	p.busy, p.deferCheck = busy.read, time.Millisecond
	defer p.Stop()
	p.Start(5)
	finishCountdown(p)
	p.UserAway(defaultAwayThreshold)
	busy.set(notBusy)
	time.Sleep(20 * time.Millisecond)
	if prompts, chimes := host.promptCount(); prompts != 0 || chimes != 0 {
		t.Fatalf("prompted after leaving: prompts=%d chimes=%d", prompts, chimes)
	}
}

func TestActivityAwareOffPromptsAtOnce(t *testing.T) {
	host := &fakeTimerHost{}
	busy := &busyScript{reason: busyTyping}
	p := NewPomodoro(nil, host, 5)
	p.busy = busy.read
	defer p.Stop()
	p.SetAutoSense(autoSense{AutoStart: true, EndOnAway: true, AwayMinutes: 5})
	p.Start(5)
	finishCountdown(p)
	if prompts, _ := host.promptCount(); prompts != 1 || busy.asked != 0 {
		t.Fatalf("prompts=%d asked=%d", prompts, busy.asked)
	}
}
