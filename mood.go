package main

import (
	"time"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/locales"
)

// catMood is how the menu bar cat looks. It tires as the user sits without
// a break and as the day's focus adds up, and sleeps through a break.
type catMood int

const (
	moodAwake catMood = iota
	moodTired
	moodWeary
	moodExhausted
	moodAsleep
)

// moodName names a tired mood in the menu bar tooltip.
func moodName(m catMood) string {
	switch m {
	case moodTired:
		return locales.MoodTired()
	case moodWeary:
		return locales.MoodWeary()
	}
	return locales.MoodExhausted()
}

// dayTiredAfter is the day's focus at which the cat looks tired, weary and
// exhausted.
var dayTiredAfter = [...]time.Duration{4 * time.Hour, 6 * time.Hour, 8 * time.Hour}

// sitting is what the presence monitor last said about the user's sitting:
// periods whole sitEvery without a break of sitBreak.
type sitting struct {
	periods  int
	sitEvery time.Duration
}

// catMoodFor is the cat's mood and the tooltip that says why. The cat is as
// tired as the more tiring of the day's focus (dayTiredAfter) and the
// sitting: each sitting reminder makes it one step more tired, and getting
// up rests it. The sitting counts only while the sitting reminder is on.
func catMoodFor(phase pomodoroPhase, todayFocus time.Duration, sat sitting) (catMood, string) {
	if phase == phaseBreak {
		return moodAsleep, locales.MoodAsleep()
	}
	day := 0
	for day < len(dayTiredAfter) && todayFocus >= dayTiredAfter[day] {
		day++
	}
	sit := 0
	if sat.sitEvery > 0 {
		sit = min(sat.periods, int(moodExhausted))
	}
	mood := catMood(max(day, sit))
	if mood == moodAwake {
		return moodAwake, ""
	}
	var why []string
	if day == int(mood) {
		why = append(why, locales.MoodWhyDay(int(dayTiredAfter[day-1].Hours())))
	}
	if sit == int(mood) {
		why = append(why, locales.MoodWhySat(int((time.Duration(sat.periods) * sat.sitEvery).Minutes())))
	}
	reason := why[0]
	if len(why) == 2 {
		reason = locales.MoodWhyBoth(why[0], why[1])
	}
	return mood, locales.MoodTooltip(moodName(mood), reason)
}
