package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
	"github.com/UncertaintyDeterminesYou4ndMe/hibi/locales"
)

type pomodoroPhase int

const (
	phaseIdle pomodoroPhase = iota
	phaseFocus
	phasePaused
	phaseBreakPending
	phaseBreak
)

const (
	breakMinutes          = 10
	breakReminderInterval = time.Minute
	breakReminderLimit    = 10
)

var focusPresets = []int{5, 15, 30, 40, 50, 60}

type pomodoroSnapshot struct {
	Phase                pomodoroPhase
	RemainingSeconds     int
	SelectedFocusMinutes int
	SessionFocusMinutes  int
	BoundTaskID          uint64
	BoundTaskTier        todo.Tier
	BoundTaskText        string
	// SessionElapsedSeconds is the counted time of the focus session that
	// runs or is paused, not yet in the focus log.
	SessionElapsedSeconds int
	// StreakSeconds is the focus of the sessions that ended since the
	// user last rested (Pomodoro.streak).
	StreakSeconds int
}

func (s pomodoroSnapshot) TimeString() string {
	seconds := max(0, s.RemainingSeconds)
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

type timerHost interface {
	TimerChanged()
	ShowBreakPrompt(focusMinutes, breakMinutes int)
	HideBreakPrompt()
	BringBreakPromptToFront()
	Chime()
	Notify(title, body string)
	SaveFocusMinutes(minutes int)
	// RecordFocus adds the counted seconds of a focus session that ended.
	RecordFocus(seconds int)
	// CheckPresence asks the presence monitor to look now: a break ended,
	// and the next session starts at once if the user is at the computer.
	CheckPresence()
}

// settlement is what an ending (or a task switch) owes: minutes to the
// bound task, and seconds to the day's focus total.
type settlement struct {
	text         string
	tier         todo.Tier
	minutes      int
	ok           bool
	focusSeconds int
}

// Pomodoro owns the timer independently of a panel window. Its lock makes
// tray/menu actions and timer goroutines safe; UI callbacks run unlocked.
//
// A running countdown (focus or break) is a deadline on the monotonic clock,
// which stands still while the computer sleeps, so sleep is not counted.
// The timer wakes only when what shows changes: each second while someone
// watches the seconds (SetFineTicks, the open panel), else each whole minute
// of the menu bar, and at the end. A ticker every second would wake the
// process 60 times a minute all day.
type Pomodoro struct {
	mu    sync.Mutex
	store *todo.Store
	host  timerHost
	clock func() time.Duration // monotonic time

	phase pomodoroPhase
	// deadline is when a running countdown reaches zero; left is the time a
	// stopped one (paused) still has.
	deadline             time.Duration
	left                 time.Duration
	fineTicks            bool
	selectedFocusMinutes int
	sessionFocusMinutes  int
	boundTaskID          uint64
	boundTaskTier        todo.Tier
	boundTaskText        string
	// boundAtRemaining is the remaining seconds when the current task was bound:
	// a task bound mid-session is credited only from then on.
	boundAtRemaining int
	// autoArmed makes UserPresent start a focus session. It is set at launch,
	// when the user leaves the computer and when a break ends; Stop clears
	// it.
	autoArmed bool
	// stopRest is set by Stop, at stoppedAt on the monotonic clock, until
	// the user leaves or a session starts. A stop leaves the user a break
	// of breakMinutes. The user who leaves takes it, and the time between
	// the stop and leaving counts as focus. The user who is still at the
	// computer when it is over did not take it: the next session starts
	// back at the stop.
	stopRest  bool
	stoppedAt time.Duration
	// streak is the counted focus seconds of the sessions that ended since
	// the user last rested: a break that ran to its end or lasted sitBreak,
	// or leaving the computer. Skipping a break keeps it, so the focus end
	// prompt can say how long the user has gone without one.
	streak int
	// autoStart and endOnAway are the settings for UserPresent and UserAway
	// (autoSense).
	autoStart bool
	endOnAway bool
	// busy says why now is a bad moment for the focus end prompt; nil is
	// never. The host sets it before the timer runs; it is asked only with
	// activityAware on (autoSense.ActivityAware). deferCheck is how often a
	// deferred prompt looks again.
	busy          func() busyReason
	activityAware bool
	deferCheck    time.Duration

	timer              *time.Timer
	timerGeneration    uint64
	reminderCancel     context.CancelFunc
	reminderGeneration uint64
	remindersPlayed    int
}

func NewPomodoro(store *todo.Store, host timerHost, selectedMinutes int) *Pomodoro {
	if selectedMinutes <= 0 {
		selectedMinutes = 40
	}
	start := time.Now()
	return &Pomodoro{
		store: store, host: host, selectedFocusMinutes: selectedMinutes, autoArmed: true,
		autoStart: true, endOnAway: true, activityAware: true, deferCheck: deferCheckInterval,
		clock: func() time.Duration { return time.Since(start) },
	}
}

func (p *Pomodoro) runningLocked() bool { return p.phase == phaseFocus || p.phase == phaseBreak }

func (p *Pomodoro) leftLocked() time.Duration {
	if p.runningLocked() {
		return max(0, p.deadline-p.clock())
	}
	return p.left
}

// remainingLocked is the time left in whole seconds, rounded up: a fresh
// 40-minute session shows 40:00 until a full second has passed.
func (p *Pomodoro) remainingLocked() int {
	return int((p.leftLocked() + time.Second - 1) / time.Second)
}

// runLocked starts the countdown of the running phase with left to go.
func (p *Pomodoro) runLocked(left time.Duration) {
	p.deadline, p.left = p.clock()+left, 0
	p.scheduleLocked()
}

// SetFineTicks makes the timer wake every second, for a view that shows
// the seconds, or only when the minute changes.
func (p *Pomodoro) SetFineTicks(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fineTicks == on {
		return
	}
	p.fineTicks = on
	if p.runningLocked() {
		p.scheduleLocked()
	}
}

func (p *Pomodoro) Snapshot() pomodoroSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	remaining := p.remainingLocked()
	s := pomodoroSnapshot{
		Phase: p.phase, RemainingSeconds: remaining,
		SelectedFocusMinutes: p.selectedFocusMinutes,
		SessionFocusMinutes:  p.sessionFocusMinutes,
		BoundTaskID:          p.boundTaskID, BoundTaskTier: p.boundTaskTier,
		BoundTaskText: p.boundTaskText, StreakSeconds: p.streak,
	}
	if p.phase == phaseFocus || p.phase == phasePaused {
		s.SessionElapsedSeconds = p.sessionFocusMinutes*60 - remaining
	}
	return s
}

func (p *Pomodoro) changed() {
	if p.host != nil {
		p.host.TimerChanged()
	}
}

type boundTask struct {
	id   uint64
	tier todo.Tier
	text string
}

func (p *Pomodoro) Start(minutes int) { p.begin(nil, minutes) }

// StartTask times item. With minutes <= 0 (the row's play button) and a
// focus session already running, it binds item to that session and keeps the
// countdown, so the break still comes after the real time at the computer.
// Otherwise it starts a new session for item.
func (p *Pomodoro) StartTask(item todo.Item, tier todo.Tier, minutes int) {
	task := &boundTask{item.ID, tier, item.Text}
	if minutes <= 0 && p.switchTask(task) {
		return
	}
	p.begin(task, minutes)
}

// switchTask binds task to the running focus session and resumes it if it is
// paused. The previously bound task is credited for the time it was bound.
// It reports false when no focus session runs.
func (p *Pomodoro) switchTask(task *boundTask) bool {
	p.mu.Lock()
	if p.phase != phaseFocus && p.phase != phasePaused {
		p.mu.Unlock()
		return false
	}
	settle := p.settleLocked(false, 0)
	settle.focusSeconds = 0 // the session goes on
	p.boundTaskID, p.boundTaskTier, p.boundTaskText = task.id, task.tier, task.text
	p.boundAtRemaining = p.remainingLocked()
	if p.phase == phasePaused {
		p.phase = phaseFocus
		p.runLocked(p.left)
	}
	p.mu.Unlock()
	p.applySettlement(settle)
	p.changed()
	return true
}

func (p *Pomodoro) begin(task *boundTask, minutes int) {
	p.mu.Lock()
	settle := p.beginLocked(task, minutes)
	p.mu.Unlock()
	p.afterBegin(settle, minutes)
}

// beginLocked starts a focus session of minutes, or of the selected length
// when minutes <= 0. It settles the session it replaces.
func (p *Pomodoro) beginLocked(task *boundTask, minutes int) settlement {
	settle := p.settleLocked(false, 0)
	p.streak += settle.focusSeconds
	p.stopTimerLocked()
	p.stopReminderLocked()
	if minutes <= 0 {
		minutes = p.selectedFocusMinutes
	}
	p.selectedFocusMinutes = minutes
	p.sessionFocusMinutes = minutes
	p.clearBindingLocked()
	if task != nil {
		p.boundTaskID, p.boundTaskTier, p.boundTaskText = task.id, task.tier, task.text
	}
	p.boundAtRemaining = minutes * 60
	p.autoArmed, p.stopRest = false, false
	p.phase = phaseFocus
	p.runLocked(time.Duration(minutes) * time.Minute)
	return settle
}

// afterBegin runs the host callbacks of a start, unlocked. Only an explicit
// length changes the remembered one.
func (p *Pomodoro) afterBegin(settle settlement, minutes int) {
	p.applySettlement(settle)
	if p.host != nil {
		if minutes > 0 {
			p.host.SaveFocusMinutes(minutes)
		}
		p.host.HideBreakPrompt()
	}
	p.changed()
}

// SetAutoSense sets whether UserPresent starts a session and whether
// UserAway ends one.
func (p *Pomodoro) SetAutoSense(s autoSense) {
	p.mu.Lock()
	p.autoStart, p.endOnAway, p.activityAware = s.AutoStart, s.EndOnAway, s.ActivityAware
	if !s.AutoStart {
		// The time after a stop counts only while auto-start is on.
		p.stopRest = false
	}
	p.mu.Unlock()
}

// UserPresent starts a focus session with the selected length when the user
// is at the computer, nothing is being timed, and auto-start is on and armed,
// or a stop's break is over (stopRest). The presence monitor calls it at
// least once a minute.
func (p *Pomodoro) UserPresent() {
	p.mu.Lock()
	if !p.autoStart || p.phase != phaseIdle {
		p.mu.Unlock()
		return
	}
	var since time.Duration // worked since a stop
	if !p.autoArmed {
		since = p.clock() - p.stoppedAt
		if !p.stopRest || since < breakMinutes*time.Minute {
			p.mu.Unlock()
			return
		}
	}
	settle := p.beginLocked(nil, 0)
	if since > 0 {
		// The session started at the stop; at least a minute is left of
		// it, and the rest of what was worked is recorded at once.
		full := time.Duration(p.sessionFocusMinutes) * time.Minute
		left := max(full-since, time.Minute)
		p.runLocked(left)
		worked := int((since - (full - left)) / time.Second)
		settle.focusSeconds += worked
		p.streak += worked
	}
	p.mu.Unlock()
	p.afterBegin(settle, 0)
}

// UserAway ends the focus session, or closes the waiting break prompt, when
// the user has left the computer: leaving counts as the break. The task is
// credited for the time before the user left. A running break keeps
// counting. Either way the next session starts when the user is back.
// With endOnAway off, leaving only arms that start.
func (p *Pomodoro) UserAway(idleFor time.Duration) {
	p.mu.Lock()
	p.streak = 0 // leaving is a rest
	if p.stopRest {
		// Left after a stop: nothing runs, and the time between the stop
		// and leaving was worked.
		worked := max(0, p.clock()-idleFor-p.stoppedAt)
		p.autoArmed, p.stopRest = true, false
		p.mu.Unlock()
		p.afterEnd(settlement{focusSeconds: int(worked / time.Second)})
		return
	}
	p.autoArmed = true
	if !p.endOnAway {
		p.mu.Unlock()
		return
	}
	var settle settlement
	switch p.phase {
	case phaseFocus:
		settle = p.settleLocked(false, int(idleFor/time.Second))
	case phasePaused:
		// A paused countdown did not count the idle time.
		settle = p.settleLocked(false, 0)
	case phaseBreakPending:
	default:
		p.mu.Unlock()
		return
	}
	p.endLocked()
	p.mu.Unlock()
	p.afterEnd(settle)
}

func (p *Pomodoro) Pause() {
	p.mu.Lock()
	if p.phase != phaseFocus {
		p.mu.Unlock()
		return
	}
	p.left = p.leftLocked()
	p.phase = phasePaused
	p.stopTimerLocked()
	p.mu.Unlock()
	p.changed()
}

func (p *Pomodoro) Resume() {
	p.mu.Lock()
	if p.phase != phasePaused {
		p.mu.Unlock()
		return
	}
	p.phase = phaseFocus
	p.runLocked(p.left)
	p.mu.Unlock()
	p.changed()
}

// Stop ends whatever runs, and leaves the user a break of breakMinutes
// (stopRest): auto-start waits for it, so a deliberate stop is not undone
// a few seconds later. Time at the computer after the stop counts as focus
// unless the user leaves. With auto-start off, nothing after a stop counts.
func (p *Pomodoro) Stop() {
	p.mu.Lock()
	// Another stop within a stop's break, as the desk API or quitting
	// sends, keeps the first one's start.
	if p.phase != phaseIdle || !p.stopRest {
		p.stoppedAt = p.clock()
	}
	settle := p.settleLocked(false, 0)
	p.streak += settle.focusSeconds
	p.endLocked()
	p.autoArmed, p.stopRest = false, p.autoStart
	p.mu.Unlock()
	p.afterEnd(settle)
}

func (p *Pomodoro) endLocked() {
	p.stopTimerLocked()
	p.stopReminderLocked()
	p.phase = phaseIdle
	p.left = 0
	p.clearBindingLocked()
}

func (p *Pomodoro) afterEnd(settle settlement) {
	p.applySettlement(settle)
	if p.host != nil {
		p.host.HideBreakPrompt()
	}
	p.changed()
}

func (p *Pomodoro) StartBreak() {
	p.mu.Lock()
	if p.phase != phaseBreakPending {
		p.mu.Unlock()
		return
	}
	p.stopReminderLocked()
	p.phase = phaseBreak
	p.runLocked(breakMinutes * time.Minute)
	p.mu.Unlock()
	if p.host != nil {
		p.host.HideBreakPrompt()
	}
	p.changed()
}

// Skip skips the break (waiting or running) and goes straight into the next
// focus session with the selected length: the user is still at the computer.
// A break skipped after sitBreak still counts as a rest.
func (p *Pomodoro) Skip() {
	p.mu.Lock()
	if p.phase != phaseBreak && p.phase != phaseBreakPending {
		p.mu.Unlock()
		return
	}
	if p.phase == phaseBreak && breakMinutes*time.Minute-p.leftLocked() >= sitBreak {
		p.streak = 0 // long enough to have got up
	}
	settle := p.beginLocked(nil, 0)
	p.mu.Unlock()
	p.afterBegin(settle, 0)
}

func (p *Pomodoro) clearBindingLocked() {
	p.boundTaskID, p.boundTaskText = 0, ""
	p.boundTaskTier = todo.Today
	p.boundAtRemaining = 0
}

// settleLocked computes what ending the focus session now owes, but does
// not alter phase or timer state. awaySeconds is counted time the user was
// not at the computer.
func (p *Pomodoro) settleLocked(natural bool, awaySeconds int) settlement {
	if p.phase != phaseFocus && p.phase != phasePaused {
		return settlement{}
	}
	remaining := p.remainingLocked()
	s := settlement{focusSeconds: max(0, p.sessionFocusMinutes*60-remaining-awaySeconds)}
	if p.boundTaskText == "" {
		return s
	}
	elapsed := max(0, p.boundAtRemaining-remaining-awaySeconds)
	// A natural completion credits the whole bound span: the full session
	// when the task was bound from the start.
	s.text, s.tier, s.ok = p.boundTaskText, p.boundTaskTier, true
	s.minutes = todo.MinutesToCredit(elapsed, natural, p.boundAtRemaining/60)
	return s
}

func (p *Pomodoro) applySettlement(s settlement) {
	if s.focusSeconds > 0 && p.host != nil {
		p.host.RecordFocus(s.focusSeconds)
	}
	if s.ok && s.minutes > 0 && p.store != nil {
		if err := p.store.CreditFocusMinutes(s.text, s.tier, s.minutes); err != nil {
			// Store mutations roll back on failure; keep the UI responsive and log
			// the error rather than losing the process at a timer boundary.
			fmt.Printf("hibi: settle focus: %v\n", err)
		}
	}
}

// scheduleLocked arms the timer for the next moment the countdown shows
// differently: the next second, or with coarse ticks the next whole minute
// (when the rounded-up minutes drop), or the end.
func (p *Pomodoro) scheduleLocked() {
	p.stopTimerLocked()
	p.timerGeneration++
	generation := p.timerGeneration
	step := time.Minute
	if p.fineTicks {
		step = time.Second
	}
	left := p.leftLocked()
	wait := left % step
	if wait == 0 {
		wait = min(step, left)
	}
	p.timer = time.AfterFunc(wait, func() { p.wake(generation) })
}

func (p *Pomodoro) stopTimerLocked() {
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

// wake runs when the scheduled moment comes: it refreshes the views, or
// ends the countdown.
func (p *Pomodoro) wake(generation uint64) {
	p.mu.Lock()
	if generation != p.timerGeneration || !p.runningLocked() {
		p.mu.Unlock()
		return
	}
	if p.leftLocked() > 0 {
		p.scheduleLocked()
		p.mu.Unlock()
		p.changed()
		return
	}

	switch p.phase {
	case phaseFocus:
		settle := p.settleLocked(true, 0)
		p.streak += settle.focusSeconds
		p.clearBindingLocked()
		p.stopTimerLocked()
		p.phase = phaseBreakPending
		p.remindersPlayed = 0
		focusMinutes := p.sessionFocusMinutes
		// While the user types or is in a call, the prompt and its chime
		// wait for a pause or for the call to end.
		reason := notBusy
		if p.busy != nil && p.activityAware {
			reason = p.busy()
		}
		p.startReminderLocked(reason, focusMinutes)
		p.mu.Unlock()
		p.applySettlement(settle)
		p.changed()
		if reason == notBusy && p.host != nil {
			p.host.Chime()
			p.host.ShowBreakPrompt(focusMinutes, breakMinutes)
		}
	case phaseBreak:
		p.stopTimerLocked()
		p.streak = 0
		p.phase = phaseIdle
		p.clearBindingLocked()
		p.autoArmed = true
		autoStart := p.autoStart
		p.mu.Unlock()
		if p.host != nil {
			body := locales.BreakEndBody()
			if autoStart {
				body = locales.BreakEndBodyAuto()
			}
			p.host.Notify(locales.BreakEndTitle(), body)
			p.host.Chime()
		}
		p.changed()
		if p.host != nil {
			p.host.CheckPresence()
		}
	default:
		p.mu.Unlock()
	}
}

// startReminderLocked brings the waiting break prompt back each
// breakReminderInterval. With a busy reason, it first shows the prompt when
// the reason ends or its limit passes.
func (p *Pomodoro) startReminderLocked(reason busyReason, focusMinutes int) {
	p.stopReminderLocked()
	p.reminderGeneration++
	generation := p.reminderGeneration
	ctx, cancel := context.WithCancel(context.Background())
	p.reminderCancel = cancel
	busy := p.busy
	go func() {
		if reason != notBusy {
			if !p.waitForMoment(ctx, reason, busy) || !p.showDeferredPrompt(generation, focusMinutes) {
				return
			}
		}
		ticker := time.NewTicker(breakReminderInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.remind(generation)
			}
		}
	}()
}

// waitForMoment waits until the user is no longer busy, or the reason's
// limit passes; a call that starts while waiting for a pause in typing gets
// the call's limit. It reports false when ctx ends first.
func (p *Pomodoro) waitForMoment(ctx context.Context, reason busyReason, busy func() busyReason) bool {
	start := p.clock()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(p.deferCheck):
		}
		now := busy()
		if now == notBusy {
			return true
		}
		reason = max(reason, now)
		if p.clock()-start >= reason.deferLimit() {
			return true
		}
	}
}

// showDeferredPrompt shows the prompt that waited, unless the break began,
// was skipped or the user left meanwhile.
func (p *Pomodoro) showDeferredPrompt(generation uint64, focusMinutes int) bool {
	p.mu.Lock()
	ok := generation == p.reminderGeneration && p.phase == phaseBreakPending
	p.mu.Unlock()
	if ok && p.host != nil {
		p.host.Chime()
		p.host.ShowBreakPrompt(focusMinutes, breakMinutes)
	}
	return ok
}

func (p *Pomodoro) stopReminderLocked() {
	if p.reminderCancel != nil {
		p.reminderCancel()
		p.reminderCancel = nil
	}
}

func (p *Pomodoro) remind(generation uint64) {
	p.mu.Lock()
	if generation != p.reminderGeneration || p.phase != phaseBreakPending {
		p.mu.Unlock()
		return
	}
	if p.remindersPlayed >= breakReminderLimit {
		p.stopReminderLocked()
		p.mu.Unlock()
		return
	}
	p.remindersPlayed++
	p.mu.Unlock()
	if p.host != nil {
		p.host.Chime()
		p.host.BringBreakPromptToFront()
	}
}
