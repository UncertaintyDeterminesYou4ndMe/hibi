package main

import (
	"slices"
	"testing"
	"time"
)

type recordedPresence struct{ events []string }

func (r *recordedPresence) UserAway(idleFor time.Duration) {
	r.events = append(r.events, "away "+idleFor.String())
}
func (r *recordedPresence) UserPresent() { r.events = append(r.events, "present") }

func TestPresenceMonitorReportsLeavingAndReturning(t *testing.T) {
	wall := time.Date(2026, 11, 14, 9, 0, 0, 0, time.Local)
	mono := time.Duration(0)
	idle := time.Duration(0)
	rec := &recordedPresence{}
	m := newPresenceMonitor(func() time.Duration { return idle }, rec)
	m.now = func() time.Time { return wall }
	m.mono = func() time.Duration { return mono }
	var waits []time.Duration
	// step lets awake time pass (both clocks), then polls.
	step := func(awake, newIdle time.Duration) {
		wall, mono, idle = wall.Add(awake), mono+awake, newIdle
		waits = append(waits, m.poll())
	}
	sleep := func(d time.Duration) { wall = wall.Add(d) } // only the wall clock runs

	step(0, 2*time.Second)                                 // here, just typed: next poll in a minute
	step(time.Minute, 62*time.Second)                      // idle a while
	step(time.Minute, defaultAwayThreshold-30*time.Second) // could be away in 30s: poll then
	step(30*time.Second, defaultAwayThreshold+time.Second) // left
	step(presencePollInterval, defaultAwayThreshold+6*time.Second)
	step(presencePollInterval, time.Second) // back
	// Working, then the lid closes for two hours; a key press wakes it.
	sleep(2 * time.Hour)
	step(time.Second, time.Second)
	// Idle three minutes, then asleep three: five minutes or more away.
	step(time.Minute, 3*time.Minute)
	sleep(3 * time.Minute)
	step(time.Second, 2*time.Second)
	// Asleep one minute after working: not away.
	sleep(time.Minute)
	step(time.Second, time.Second)

	want := []string{
		"present", "present", "present", "away 5m1s", "present",
		"away 1s", "present",
		"present", "away 3m0s", "present",
		"present",
	}
	if !slices.Equal(rec.events, want) {
		t.Fatalf("events =\n%q\nwant\n%q", rec.events, want)
	}
	// Present: until the user could first count as away, at most a minute.
	// Away: every few seconds, to start promptly on their return.
	wantWaits := []time.Duration{
		time.Minute, time.Minute, 30 * time.Second, presencePollInterval, presencePollInterval,
		time.Minute, time.Minute, time.Minute, time.Minute, time.Minute,
	}
	if !slices.Equal(waits, wantWaits) {
		t.Fatalf("waits = %v\nwant %v", waits, wantWaits)
	}
}

func TestPresenceKickNeverBlocks(t *testing.T) {
	m := newPresenceMonitor(func() time.Duration { return 0 }, &recordedPresence{})
	m.Kick()
	m.Kick() // a kick already waiting is enough
	if len(m.kick) != 1 {
		t.Fatalf("pending kicks=%d", len(m.kick))
	}
}

func TestPresenceThresholdFollowsTheSetting(t *testing.T) {
	idle := 4 * time.Minute
	rec := &recordedPresence{}
	m := newPresenceMonitor(func() time.Duration { return idle }, rec)
	m.poll() // under five minutes: present
	m.SetThreshold(3 * time.Minute)
	if len(m.kick) != 1 {
		t.Fatal("a new threshold did not ask for a poll")
	}
	if wait := m.poll(); wait != presencePollInterval {
		t.Fatalf("wait while away=%v", wait)
	}
	want := []string{"present", "away 4m0s"}
	if !slices.Equal(rec.events, want) {
		t.Fatalf("events=%q want %q", rec.events, want)
	}
}

func TestAutoSensePreferences(t *testing.T) {
	off, on := false, true
	for _, tc := range []struct {
		name  string
		prefs preferences
		want  autoSense
	}{
		{"missing block", preferences{}, defaultAutoSense()},
		{"missing fields", preferences{AutoSense: &autoSensePrefs{}}, defaultAutoSense()},
		{"all set", preferences{AutoSense: &autoSensePrefs{AutoStart: &off, EndOnAway: &on, AwayMinutes: 15, SitReminder: &off, SitMinutes: 45, ActivityAware: &off}},
			autoSense{AutoStart: false, EndOnAway: true, AwayMinutes: 15, SitReminder: false, SitMinutes: 45, ActivityAware: false}},
		{"unknown minutes", preferences{AutoSense: &autoSensePrefs{AwayMinutes: 7, SitMinutes: 20}}, defaultAutoSense()},
	} {
		if got := tc.prefs.autoSense(); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if (autoSense{}).watches() {
		t.Error("all off still watches")
	}
	if !(autoSense{SitReminder: true}).watches() {
		t.Error("the sitting reminder alone does not watch")
	}
}

func TestSitReminder(t *testing.T) {
	wall := time.Date(2026, 11, 16, 9, 0, 0, 0, time.Local)
	mono := time.Duration(0)
	idle := time.Duration(0)
	var sat []time.Duration
	m := newPresenceMonitor(func() time.Duration { return idle }, &recordedPresence{})
	m.now = func() time.Time { return wall }
	m.mono = func() time.Duration { return mono }
	m.onSit = func(d time.Duration) { sat = append(sat, d) }
	m.sitEvery.Store(int64(30 * time.Minute))
	// sit polls as the monitor asks, typing a key now and then (idle stays
	// under a minute), until until has passed.
	sit := func(until time.Duration) {
		for end := mono + until; mono < end; {
			wait := m.poll()
			wall, mono, idle = wall.Add(wait), mono+wait, (idle+wait)%time.Minute
		}
	}
	sit(65 * time.Minute)
	if len(sat) != 2 || sat[0] < 30*time.Minute || sat[0] > 31*time.Minute || sat[1] < 60*time.Minute || sat[1] > 61*time.Minute {
		t.Fatalf("reminders at %v, want about 30m and 60m", sat)
	}

	// A walk of three minutes without input: sitting starts over.
	idle = sitBreak
	m.poll()
	idle = 0
	sat = nil
	sit(29 * time.Minute)
	if len(sat) != 0 {
		t.Fatalf("reminded %v within 29 minutes of the walk", sat)
	}
	sit(2 * time.Minute)
	if len(sat) != 1 {
		t.Fatalf("no reminder 30 minutes after the walk: %v", sat)
	}

	// Asleep: sitting starts over too.
	wall = wall.Add(time.Hour)
	idle = 0
	sat = nil
	sit(29 * time.Minute)
	if len(sat) != 0 {
		t.Fatalf("reminded %v within 29 minutes of waking", sat)
	}

	// Off: no reminders, and the monitor waits only for the away threshold.
	m.sitEvery.Store(0)
	sat = nil
	sit(2 * time.Hour)
	if len(sat) != 0 {
		t.Fatalf("reminded with the reminder off: %v", sat)
	}
}

func TestSitReminderPollsWhenTheWalkWouldCount(t *testing.T) {
	idle := 150 * time.Second
	m := newPresenceMonitor(func() time.Duration { return idle }, &recordedPresence{})
	m.sitEvery.Store(int64(30 * time.Minute))
	if wait := m.poll(); wait != sitBreak-idle {
		t.Fatalf("wait=%v, want %v: the poll when the walk could count", wait, sitBreak-idle)
	}
}

func TestVideoOrCallHoldsPresence(t *testing.T) {
	var hold presenceHold
	asked := 0
	watching := func(on bool) func() bool { return func() bool { asked++; return on } }
	if got := hold.held(10*time.Second, true, watching(true)); got != 10*time.Second || asked != 0 {
		t.Fatalf("with recent input: idle=%v asked=%d", got, asked)
	}
	if got := hold.held(10*time.Minute, false, watching(true)); got != 10*time.Minute || asked != 0 {
		t.Fatalf("setting off: idle=%v asked=%d", got, asked)
	}
	if got := hold.held(10*time.Minute, true, watching(false)); got != 10*time.Minute {
		t.Fatalf("nothing plays: idle=%v", got)
	}
	if got := hold.held(10*time.Minute, true, watching(true)); got != 0 {
		t.Fatalf("a video plays: idle=%v", got)
	}
	// It ended: the hold lasts deskMotionHold, then the idle time counts
	// from the last time it was seen.
	if got := hold.held(11*time.Minute, true, watching(false)); got != 0 {
		t.Fatalf("just after: idle=%v", got)
	}
}
