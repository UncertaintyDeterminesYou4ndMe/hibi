package main

import (
	"context"
	"sync/atomic"
	"time"
)

const (
	// presencePollInterval is how often Hibi reads the keyboard and mouse
	// idle time while the user is away, so a session starts within seconds
	// of their return.
	presencePollInterval = 5 * time.Second
	// presenceMaxInterval caps the wait while the user is at the computer.
	presenceMaxInterval = time.Minute
	// defaultAwayThreshold is how long without keyboard or mouse input (or
	// asleep) counts as having left the computer, unless the settings say
	// otherwise.
	defaultAwayThreshold = 5 * time.Minute
	// sitBreak without input (or asleep) counts as having got up from the
	// chair, which starts the sitting time over.
	sitBreak = 3 * time.Minute
	// defaultSitMinutes is how long the user may sit before the sitting
	// reminder, unless the settings say otherwise. A five-minute walk every
	// half hour is the dose that lowered both blood sugar and blood
	// pressure in Duran et al. 2023; one every hour did not lower blood
	// sugar.
	defaultSitMinutes = 30
)

// awayPresets are the away thresholds, in minutes, the settings offer.
var awayPresets = []int{3, 5, 10, 15, 30}

// sitPresets are the sitting times, in minutes, the settings offer.
var sitPresets = []int{30, 45, 60}

// autoSense is what the presence monitor's signals do, as the settings
// set it.
type autoSense struct {
	// AutoStart starts a focus session when the user is at the computer.
	AutoStart bool
	// EndOnAway ends the focus session when the user leaves, which counts
	// as the break.
	EndOnAway bool
	// AwayMinutes without input counts as having left the computer.
	AwayMinutes int
	// SitReminder reminds the user to get up after SitMinutes at the
	// computer without a break of sitBreak.
	SitReminder bool
	SitMinutes  int
	// ActivityAware reads from the system whether a video plays, a call
	// goes on or the user types (activity.go): watching or in a call counts
	// as at the computer, and a prompt waits for a pause in typing or for
	// the call to end.
	ActivityAware bool
}

func defaultAutoSense() autoSense {
	return autoSense{
		AutoStart: true, EndOnAway: true, AwayMinutes: int(defaultAwayThreshold / time.Minute),
		SitReminder: true, SitMinutes: defaultSitMinutes, ActivityAware: true,
	}
}

// watchesAway reports whether leaving the computer does anything.
func (s autoSense) watchesAway() bool { return s.AutoStart || s.EndOnAway }

// watches reports whether any setting needs the idle time, that is whether
// Hibi reads it at all.
func (s autoSense) watches() bool { return s.watchesAway() || s.SitReminder }

// sitEvery is the sitting time before a reminder; 0 is no reminder.
func (s autoSense) sitEvery() time.Duration {
	if !s.SitReminder {
		return 0
	}
	return time.Duration(s.SitMinutes) * time.Minute
}

// presenceTarget receives what presenceMonitor sees. Pomodoro implements it.
type presenceTarget interface {
	// UserAway is called once when the user has left the computer. idleFor is
	// the awake time without input that the focus countdown already counted.
	UserAway(idleFor time.Duration)
	// UserPresent is called on every poll while the user is at the computer.
	UserPresent()
}

// presenceMonitor turns the system's keyboard and mouse idle time into two
// signals for the timer: the user left the computer, and the user is at it.
// It also tells onSit when the user has sat at the computer too long.
//
// While the user is here it reads the idle time only when something could
// next change (they could count as away or as having got up, or a sitting
// reminder is due; at most a minute on), not every few seconds: each read
// wakes the process. Kick makes it
// read at once, for the moments that cannot wait: the computer woke, the
// screen unlocked, a break ended. Only its own goroutine touches its
// fields, except kick.
type presenceMonitor struct {
	idle   func() time.Duration
	now    func() time.Time     // wall clock: it runs on while the computer sleeps
	mono   func() time.Duration // monotonic clock: it stands still meanwhile
	target presenceTarget
	kick   chan struct{}
	// threshold is the away threshold; SetThreshold changes it.
	threshold atomic.Int64
	// sitEvery is the sitting time before onSit, 0 for none; SetSitEvery
	// changes it.
	sitEvery atomic.Int64
	// onSit is told how long the user has sat, each sitEvery of sitting.
	onSit func(sat time.Duration)
	// onSitPeriods is told how many whole sitEvery the user has sat since
	// the last break, each time that number changes: up at every onSit,
	// and back to 0 when the user gets up.
	onSitPeriods func(periods int, sitEvery time.Duration)

	away     bool
	polled   bool
	lastWall time.Time
	lastMono time.Duration
	lastIdle time.Duration
	// sitting is whether the user has been at the computer since sitSince
	// (monotonic) without a break of sitBreak; sitReminders counts the
	// reminders since.
	sitting      bool
	sitSince     time.Duration
	sitReminders int
	sitPeriods   int // last told onSitPeriods
}

func newPresenceMonitor(idle func() time.Duration, target presenceTarget) *presenceMonitor {
	start := time.Now()
	m := &presenceMonitor{
		idle: idle, now: time.Now, target: target,
		mono: func() time.Duration { return time.Since(start) },
		kick: make(chan struct{}, 1),
	}
	m.threshold.Store(int64(defaultAwayThreshold))
	return m
}

// SetThreshold sets how long without input counts as away, and polls at
// once with it.
func (m *presenceMonitor) SetThreshold(d time.Duration) {
	m.threshold.Store(int64(d))
	m.Kick()
}

// SetSitEvery sets how long the user may sit before onSit, 0 for never,
// and polls at once with it.
func (m *presenceMonitor) SetSitEvery(d time.Duration) {
	m.sitEvery.Store(int64(d))
	m.Kick()
}

// Kick asks for a poll now. It never blocks.
func (m *presenceMonitor) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

func (m *presenceMonitor) run(ctx context.Context) {
	timer := time.NewTimer(m.poll())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-m.kick:
		}
		timer.Reset(m.poll())
	}
}

// poll reads the idle time, tells the target what changed, and returns how
// long to wait before the next poll.
func (m *presenceMonitor) poll() time.Duration {
	wall, mono := m.now().Round(0), m.mono()
	idle := m.idle()
	awayThreshold := time.Duration(m.threshold.Load())
	sitEvery := time.Duration(m.sitEvery.Load())
	// The wall clock minus the monotonic clock since the last poll is how
	// long the computer slept. Asleep counts as away (and as having got
	// up), together with the idle time before it; the focus countdown
	// counted only the latter.
	var slept time.Duration
	lastIdle := m.lastIdle
	if m.polled {
		slept = max(0, wall.Sub(m.lastWall)-(mono-m.lastMono))
	}
	if m.polled && !m.away && slept > 0 && slept+lastIdle >= awayThreshold {
		m.away = true
		m.target.UserAway(lastIdle)
	}
	if idle >= awayThreshold {
		if !m.away {
			m.away = true
			m.target.UserAway(idle)
		}
	} else {
		m.away = false
		m.target.UserPresent()
	}
	m.polled = true
	m.lastWall, m.lastMono, m.lastIdle = wall, mono, idle

	// Sitting: from the last input after a break, to the next break.
	gotUp := m.away || idle >= sitBreak || (slept > 0 && slept+lastIdle >= sitBreak)
	if sitEvery <= 0 || gotUp {
		m.sitting = false
	} else {
		if !m.sitting {
			m.sitting, m.sitSince, m.sitReminders = true, mono-idle, 0
		}
		if sat := mono - m.sitSince; sat >= sitEvery*time.Duration(m.sitReminders+1) {
			m.sitReminders++
			if m.onSit != nil {
				m.onSit(sat)
			}
		}
	}
	periods := 0
	if m.sitting {
		periods = m.sitReminders
	}
	if periods != m.sitPeriods {
		m.sitPeriods = periods
		if m.onSitPeriods != nil {
			m.onSitPeriods(periods, sitEvery)
		}
	}

	if m.away {
		return presencePollInterval
	}
	wait := awayThreshold - idle
	if sitEvery > 0 {
		if idle < sitBreak {
			wait = min(wait, sitBreak-idle)
		}
		if m.sitting {
			wait = min(wait, m.sitSince+sitEvery*time.Duration(m.sitReminders+1)-mono)
		}
	}
	return min(max(wait, presencePollInterval), presenceMaxInterval)
}
