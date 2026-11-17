// Package todo implements Hibi's Markdown-backed task model without any UI
// dependencies, so parsing, persistence and timer settlement stay testable.
package todo

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// Quadrant is an Eisenhower priority marker. Zero means unclassified.
type Quadrant int

const (
	QuadrantNone Quadrant = iota
	Quadrant1
	Quadrant2
	Quadrant3
	Quadrant4
)

// Tier is one of the three time horizons shown by Hibi.
type Tier int

const (
	Today Tier = iota
	ThreeDay
	Week
)

func (t Tier) String() string {
	switch t {
	case Today:
		return "今日"
	case ThreeDay:
		return "三日"
	case Week:
		return "本周"
	default:
		return "未知"
	}
}

var nextItemID atomic.Uint64

// Item is one Markdown task. ID is deliberately session-local: the file format
// stays plain Markdown and parsing a genuine external edit mints fresh IDs.
type Item struct {
	ID           uint64
	Text         string
	Done         bool
	Quadrant     Quadrant
	FocusMinutes int
	Tags         []string
}

func NewItem(text string) Item {
	return Item{ID: nextItemID.Add(1), Text: text}
}

func (i Item) SameContent(other Item) bool {
	return i.Text == other.Text && i.Done == other.Done &&
		i.Quadrant == other.Quadrant && i.FocusMinutes == other.FocusMinutes &&
		slices.Equal(i.Tags, other.Tags)
}

// EditableText omits the timer-owned duration marker, matching quick-add input.
func (i Item) EditableText() string {
	parts := []string{i.Text}
	for _, tag := range i.Tags {
		parts = append(parts, "#"+tag)
	}
	if i.Quadrant != QuadrantNone {
		parts = append(parts, fmt.Sprintf("!%d", i.Quadrant))
	}
	return strings.Join(parts, " ")
}

// Data is the complete interoperable contents of ~/todos.md.
type Data struct {
	TodayDate     string
	TodayItems    []Item
	ThreeDayItems []Item
	WeekItems     []Item
	ArchiveBody   string
}

func Empty(todayDate string) Data {
	return Data{TodayDate: todayDate}
}

func (d Data) Clone() Data {
	cloneItems := func(items []Item) []Item {
		if items == nil {
			return nil
		}
		out := make([]Item, len(items))
		for i, item := range items {
			out[i] = item
			out[i].Tags = slices.Clone(item.Tags)
		}
		return out
	}
	d.TodayItems = cloneItems(d.TodayItems)
	d.ThreeDayItems = cloneItems(d.ThreeDayItems)
	d.WeekItems = cloneItems(d.WeekItems)
	return d
}

func (d Data) SameContent(other Data) bool {
	if d.TodayDate != other.TodayDate || d.ArchiveBody != other.ArchiveBody {
		return false
	}
	same := func(a, b []Item) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if !a[i].SameContent(b[i]) {
				return false
			}
		}
		return true
	}
	return same(d.TodayItems, other.TodayItems) &&
		same(d.ThreeDayItems, other.ThreeDayItems) && same(d.WeekItems, other.WeekItems)
}

func (d *Data) Items(tier Tier) *[]Item {
	switch tier {
	case Today:
		return &d.TodayItems
	case ThreeDay:
		return &d.ThreeDayItems
	case Week:
		return &d.WeekItems
	default:
		panic("invalid todo tier")
	}
}

func TodayDate(now time.Time) string { return now.Format("2006-01-02") }

func HeaderDisplay(dateString string) string {
	date, err := time.ParseInLocation("2006-01-02", dateString, time.Local)
	if err != nil {
		return dateString
	}
	weekdays := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return fmt.Sprintf("%d月%d日 %s", int(date.Month()), date.Day(), weekdays[date.Weekday()])
}

// Sorted returns display order without changing serialized file order.
func Sorted(items []Item) []Item {
	out := slices.Clone(items)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Done != out[j].Done {
			return !out[i].Done
		}
		rank := func(q Quadrant) int {
			if q >= Quadrant1 && q <= Quadrant4 {
				return int(q) - 1
			}
			return 4
		}
		return rank(out[i].Quadrant) < rank(out[j].Quadrant)
	})
	return out
}

// Rollover carries unfinished tasks forward and prepends one archive block
// containing all completed tasks when the today section's date changes.
func Rollover(input Data, now time.Time) (Data, bool) {
	data := input.Clone()
	current := TodayDate(now)
	if data.TodayDate == current {
		return data, false
	}
	oldDate := data.TodayDate
	var archived []Item
	for _, tier := range []Tier{Today, ThreeDay, Week} {
		items := data.Items(tier)
		kept := (*items)[:0]
		for _, item := range *items {
			if item.Done {
				archived = append(archived, item)
			} else {
				kept = append(kept, item)
			}
		}
		*items = kept
	}
	if len(archived) > 0 {
		if oldDate == "" {
			oldDate = current
		}
		lines := []string{"### " + oldDate, ""}
		for _, item := range archived {
			lines = append(lines, SerializeItem(item))
		}
		block := strings.Join(lines, "\n")
		if data.ArchiveBody == "" {
			data.ArchiveBody = block
		} else {
			data.ArchiveBody = block + "\n\n" + data.ArchiveBody
		}
	}
	data.TodayDate = current
	return data, true
}

// MinutesToCredit credits a naturally completed session in full; an early stop
// credits only whole elapsed minutes.
func MinutesToCredit(elapsedSeconds int, completedNaturally bool, sessionMinutes int) int {
	if completedNaturally {
		return sessionMinutes
	}
	return max(0, elapsedSeconds/60)
}

// ApplyCredit updates the first exact text match, as the original file-backed
// implementation does when IDs have changed after an external edit.
func ApplyCredit(items []Item, matchingText string, minutes int) []Item {
	out := slices.Clone(items)
	if minutes <= 0 {
		return out
	}
	for i := range out {
		if out[i].Text == matchingText {
			out[i].FocusMinutes += minutes
			break
		}
	}
	return out
}
