package todo

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSerializeRoundTrip(t *testing.T) {
	first := NewItem("未完成任务")
	second := NewItem("已完成任务")
	second.Done = true
	three := NewItem("三日任务")
	three.Quadrant = Quadrant2
	week := NewItem("本周任务")
	data := Data{
		TodayDate: "2026-07-17", TodayItems: []Item{first, second},
		ThreeDayItems: []Item{three}, WeekItems: []Item{week},
		ArchiveBody: "### 2026-07-16\n\n- [x] 昨天完成的任务",
	}
	serialized := Serialize(data)
	parsed := Parse(serialized)
	if !parsed.SameContent(data) {
		t.Fatalf("round trip mismatch:\n%s\n%#v\n%#v", serialized, data, parsed)
	}
	for _, header := range []string{"## 三日\n", "## 本周\n"} {
		if !strings.Contains(serialized, header) {
			t.Errorf("missing canonical header %q", header)
		}
	}
}

func TestLegacyWeekHeader(t *testing.T) {
	input := "# Todos\n\n## 今日 2026-07-17\n\n- [ ] 任务A !1\n- [x] 任务B\n\n## 三日\n\n- [ ] 任务C !2\n\n## 本周 2026-W29\n\n- [ ] 任务D\n\n## 归档\n\n### 2026-07-16\n\n- [x] 昨天完成的\n"
	data := Parse(input)
	if data.TodayDate != "2026-07-17" || len(data.TodayItems) != 2 ||
		len(data.ThreeDayItems) != 1 || len(data.WeekItems) != 1 {
		t.Fatalf("legacy data lost: %#v", data)
	}
	if data.TodayItems[0].Quadrant != Quadrant1 || data.WeekItems[0].Text != "任务D" {
		t.Fatalf("legacy fields wrong: %#v", data)
	}
	if !strings.Contains(data.ArchiveBody, "昨天完成的") {
		t.Fatal("archive not preserved")
	}
	out := Serialize(data)
	if !strings.Contains(out, "## 本周\n") || strings.Contains(out, "2026-W29") {
		t.Fatalf("week header not normalized:\n%s", out)
	}
}

func TestSuffixMarkers(t *testing.T) {
	tests := []struct {
		input, text string
		quadrant    Quadrant
		minutes     int
		hasMinutes  bool
	}{
		{"任务 !1 ⏱85m", "任务", Quadrant1, 85, true},
		{"任务 ⏱85m !1", "任务", Quadrant1, 85, true},
		{"修 bug ⏱40m", "修 bug", QuadrantNone, 40, true},
		{"11需求!1", "11需求", Quadrant1, 0, false},
		{"清零 ⏱0m", "清零", QuadrantNone, 0, true},
		{"任务 ⏱40m 备注 !2 结尾", "任务 ⏱40m 备注 !2 结尾", QuadrantNone, 0, false},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			text, suffixes := StripSuffixMarkers(test.input)
			if text != test.text || suffixes.Quadrant != test.quadrant ||
				suffixes.FocusMinutes != test.minutes || suffixes.HasFocusMinutes != test.hasMinutes {
				t.Fatalf("got text=%q suffixes=%#v", text, suffixes)
			}
		})
	}
	if got := SerializeItem(Item{Text: "任务", Quadrant: Quadrant1, FocusMinutes: 85}); got != "- [ ] 任务 !1 ⏱85m" {
		t.Fatalf("canonical serialization: %q", got)
	}
}

func TestTags(t *testing.T) {
	text, tags := ExtractTags("  写 #work 报告 #home #work # ")
	if text != "写 报告 #" || !reflect.DeepEqual(tags, []string{"work", "home"}) {
		t.Fatalf("text=%q tags=%v", text, tags)
	}
	item, ok := ParseInput(" #work !2 ")
	if ok || item.Text != "" {
		t.Fatal("input without visible text must be rejected")
	}
}

func TestSortedIsStableAndDoesNotMutate(t *testing.T) {
	items := []Item{
		{Text: "plain"}, {Text: "q3", Quadrant: Quadrant3},
		{Text: "done", Done: true, Quadrant: Quadrant1},
		{Text: "q1-a", Quadrant: Quadrant1}, {Text: "q1-b", Quadrant: Quadrant1},
	}
	got := Sorted(items)
	var order []string
	for _, item := range got {
		order = append(order, item.Text)
	}
	want := []string{"q1-a", "q1-b", "q3", "plain", "done"}
	if !reflect.DeepEqual(order, want) || items[0].Text != "plain" {
		t.Fatalf("order=%v input=%v", order, items)
	}
}
