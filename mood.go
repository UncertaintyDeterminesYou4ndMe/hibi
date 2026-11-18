package main

import (
	"fmt"
	"strings"
	"time"
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

// moodNames name the tired moods in the menu bar tooltip.
var moodNames = map[catMood]string{moodTired: "有点累了", moodWeary: "很累了", moodExhausted: "累坏了"}

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
		return moodAsleep, "休息中"
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
		why = append(why, fmt.Sprintf("今天已经专注 %d 小时", int(dayTiredAfter[day-1].Hours())))
	}
	if sit == int(mood) {
		why = append(why, fmt.Sprintf("已经连续坐了 %d 分钟", int((time.Duration(sat.periods)*sat.sitEvery).Minutes())))
	}
	return mood, moodNames[mood] + "：" + strings.Join(why, "，")
}
