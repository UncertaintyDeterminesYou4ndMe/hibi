package todo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	todayHeader       = regexp.MustCompile(`^##\s*今日\s+(.+?)\s*$`)
	threeDayHeader    = regexp.MustCompile(`^##\s*三日\s*$`)
	weekHeader        = regexp.MustCompile(`^##\s*本周(\s|$)`)
	archiveHeader     = regexp.MustCompile(`^##\s*归档\s*$`)
	openItem          = regexp.MustCompile(`^-\s*\[\s\]\s*(.*)$`)
	doneItem          = regexp.MustCompile(`^-\s*\[[xX]\]\s*(.*)$`)
	quadrantSuffix    = regexp.MustCompile(`^(.*?)\s*!([1-4])$`)
	durationSuffix    = regexp.MustCompile(`^(.*?)\s*⏱(\d+)m$`)
	tagPattern        = regexp.MustCompile(`#[^\s#]+`)
	whitespacePattern = regexp.MustCompile(`\s+`)
)

// Parse reads Hibi's Markdown format. Archive contents remain raw and all
// legacy week IDs are accepted then normalized by Serialize.
func Parse(text string) Data {
	var data Data
	lines := strings.Split(text, "\n")
	section := -1
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if match := todayHeader.FindStringSubmatch(line); match != nil {
			data.TodayDate = match[1]
			section = int(Today)
			continue
		}
		if threeDayHeader.MatchString(line) {
			section = int(ThreeDay)
			continue
		}
		if weekHeader.MatchString(line) {
			section = int(Week)
			continue
		}
		if archiveHeader.MatchString(line) {
			data.ArchiveBody = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			break
		}
		item, ok := ParseItemLine(line)
		if !ok {
			continue
		}
		switch Tier(section) {
		case Today:
			data.TodayItems = append(data.TodayItems, item)
		case ThreeDay:
			data.ThreeDayItems = append(data.ThreeDayItems, item)
		case Week:
			data.WeekItems = append(data.WeekItems, item)
		}
	}
	return data
}

func ParseItemLine(line string) (Item, bool) {
	var text string
	var done bool
	if match := openItem.FindStringSubmatch(line); match != nil {
		text = match[1]
	} else if match := doneItem.FindStringSubmatch(line); match != nil {
		text, done = match[1], true
	} else {
		return Item{}, false
	}
	remaining, suffixes := StripSuffixMarkers(text)
	clean, tags := ExtractTags(remaining)
	item := NewItem(clean)
	item.Done = done
	item.Quadrant = suffixes.Quadrant
	item.FocusMinutes = suffixes.FocusMinutes
	item.Tags = tags
	return item, true
}

// Suffixes are the optional markers recognized only at the end of task text.
type Suffixes struct {
	Quadrant        Quadrant
	FocusMinutes    int
	HasFocusMinutes bool
}

func StripQuadrantSuffix(text string) (string, Quadrant) {
	match := quadrantSuffix.FindStringSubmatch(text)
	if match == nil {
		return text, QuadrantNone
	}
	value, _ := strconv.Atoi(match[2])
	return match[1], Quadrant(value)
}

func StripDurationSuffix(text string) (string, int, bool) {
	match := durationSuffix.FindStringSubmatch(text)
	if match == nil {
		return text, 0, false
	}
	minutes, err := strconv.Atoi(match[2])
	if err != nil {
		return text, 0, false
	}
	return match[1], minutes, true
}

func StripSuffixMarkers(text string) (string, Suffixes) {
	remaining := text
	var suffixes Suffixes
	for {
		progress := false
		if suffixes.Quadrant == QuadrantNone {
			clean, quadrant := StripQuadrantSuffix(remaining)
			if quadrant != QuadrantNone {
				remaining, suffixes.Quadrant, progress = clean, quadrant, true
			}
		}
		if !suffixes.HasFocusMinutes {
			clean, minutes, ok := StripDurationSuffix(remaining)
			if ok {
				remaining, suffixes.FocusMinutes, suffixes.HasFocusMinutes = clean, minutes, true
				progress = true
			}
		}
		if !progress {
			return remaining, suffixes
		}
	}
}

// ExtractTags removes #tag tokens, preserves first-occurrence order, and
// de-duplicates them. A lone # is ordinary text.
func ExtractTags(text string) (string, []string) {
	matches := tagPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	seen := make(map[string]struct{}, len(matches))
	tags := make([]string, 0, len(matches))
	for _, match := range matches {
		tag := text[match[0]+1 : match[1]]
		if _, exists := seen[tag]; !exists {
			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
	}
	clean := tagPattern.ReplaceAllString(text, " ")
	clean = strings.TrimSpace(whitespacePattern.ReplaceAllString(clean, " "))
	return clean, tags
}

// ParseInput applies the quick-add grammar. It rejects input with no visible
// task text after removing tags and trailing markers.
func ParseInput(raw string) (Item, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Item{}, false
	}
	remaining, suffixes := StripSuffixMarkers(trimmed)
	clean, tags := ExtractTags(remaining)
	if clean == "" {
		return Item{}, false
	}
	item := NewItem(clean)
	item.Quadrant = suffixes.Quadrant
	item.FocusMinutes = suffixes.FocusMinutes
	item.Tags = tags
	return item, true
}

func SerializeItem(item Item) string {
	var b strings.Builder
	if item.Done {
		b.WriteString("- [x] ")
	} else {
		b.WriteString("- [ ] ")
	}
	b.WriteString(item.Text)
	for _, tag := range item.Tags {
		b.WriteString(" #")
		b.WriteString(tag)
	}
	if item.Quadrant != QuadrantNone {
		fmt.Fprintf(&b, " !%d", item.Quadrant)
	}
	if item.FocusMinutes > 0 {
		fmt.Fprintf(&b, " ⏱%dm", item.FocusMinutes)
	}
	return b.String()
}

func Serialize(data Data) string {
	lines := []string{"# Todos", "", "## 今日 " + data.TodayDate, ""}
	for _, item := range data.TodayItems {
		lines = append(lines, SerializeItem(item))
	}
	lines = append(lines, "", "## 三日", "")
	for _, item := range data.ThreeDayItems {
		lines = append(lines, SerializeItem(item))
	}
	lines = append(lines, "", "## 本周", "")
	for _, item := range data.WeekItems {
		lines = append(lines, SerializeItem(item))
	}
	lines = append(lines, "", "## 归档", "")
	if data.ArchiveBody != "" {
		lines = append(lines, data.ArchiveBody)
	}
	result := strings.Join(lines, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}
