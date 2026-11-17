package todo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRollover(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.Local)
	data := Data{
		TodayDate:     "2026-07-17",
		TodayItems:    []Item{{Text: "keep"}, {Text: "today done", Done: true}},
		ThreeDayItems: []Item{{Text: "three done", Done: true, Quadrant: Quadrant2}},
		WeekItems:     []Item{{Text: "week keep"}, {Text: "week done", Done: true}},
		ArchiveBody:   "### old\n\n- [x] existing",
	}
	rolled, changed := Rollover(data, now)
	if !changed || rolled.TodayDate != "2026-07-18" || len(rolled.TodayItems) != 1 ||
		len(rolled.ThreeDayItems) != 0 || len(rolled.WeekItems) != 1 {
		t.Fatalf("bad rollover: %#v", rolled)
	}
	for _, text := range []string{"### 2026-07-17", "today done", "three done", "week done", "existing"} {
		if !strings.Contains(rolled.ArchiveBody, text) {
			t.Errorf("archive missing %q", text)
		}
	}
	if len(data.TodayItems) != 2 || len(data.ThreeDayItems) != 1 {
		t.Fatal("rollover mutated input")
	}
	if same, changed := Rollover(rolled, now); changed || !same.SameContent(rolled) {
		t.Fatal("same-day rollover should be a no-op")
	}
}

func TestFileCreationAndRollover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "todos.md")
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.Local)
	data, err := LoadWithRollover(path, now)
	if err != nil || data.TodayDate != "2026-07-18" {
		t.Fatalf("create: data=%#v err=%v", data, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	old := data
	old.TodayDate = "2026-07-17"
	done := NewItem("finished")
	done.Done = true
	old.TodayItems = []Item{done}
	if err := Save(old, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadWithRollover(path, now)
	if err != nil || len(loaded.TodayItems) != 0 || !strings.Contains(loaded.ArchiveBody, "finished") {
		t.Fatalf("reload: data=%#v err=%v", loaded, err)
	}
}

func TestStoreActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todos.md")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	changes := 0
	store.SetOnChange(func() { changes++ })
	if err := store.Add("写报告 #work #work !1", Today); err != nil {
		t.Fatal(err)
	}
	data := store.Snapshot()
	if len(data.TodayItems) != 1 {
		t.Fatal("add failed")
	}
	item := data.TodayItems[0]
	if item.Text != "写报告" || item.Quadrant != Quadrant1 || len(item.Tags) != 1 {
		t.Fatalf("added item=%#v", item)
	}
	if err := store.Toggle(item.ID, Today); err != nil {
		t.Fatal(err)
	}
	if err := store.SetQuadrant(item.ID, Quadrant3, Today); err != nil {
		t.Fatal(err)
	}
	if err := store.Move(item.ID, Today, Week); err != nil {
		t.Fatal(err)
	}
	changed, err := store.UpdateText(item.ID, "新正文 #next ⏱0m", Week)
	if err != nil || !changed {
		t.Fatalf("update changed=%v err=%v", changed, err)
	}
	if err := store.CreditFocusMinutes("新正文", Week, 5); err != nil {
		t.Fatal(err)
	}
	item = store.Snapshot().WeekItems[0]
	if !item.Done || item.Text != "新正文" || item.Quadrant != QuadrantNone ||
		item.FocusMinutes != 5 || len(item.Tags) != 1 || item.Tags[0] != "next" {
		t.Fatalf("updated item=%#v", item)
	}
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	if changes != 6 {
		t.Fatalf("watcher echo triggered a change: %d", changes)
	}
	if err := store.Delete(item.ID, Week); err != nil || len(store.Snapshot().WeekItems) != 0 {
		t.Fatalf("delete err=%v", err)
	}
}

func TestFocusSettlement(t *testing.T) {
	if got := MinutesToCredit(125, false, 40); got != 2 {
		t.Fatalf("early credit=%d", got)
	}
	if got := MinutesToCredit(0, true, 40); got != 40 {
		t.Fatalf("natural credit=%d", got)
	}
	items := []Item{{Text: "same", FocusMinutes: 3}, {Text: "same"}}
	got := ApplyCredit(items, "same", 2)
	if got[0].FocusMinutes != 5 || got[1].FocusMinutes != 0 || items[0].FocusMinutes != 3 {
		t.Fatalf("credit=%v input=%v", got, items)
	}
}
