package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
	"github.com/egoist/mygo/ui"
)

func newPanelTestApp(t *testing.T) (*app, *ui.Tester) {
	t.Helper()
	store, err := todo.OpenStore(filepath.Join(t.TempDir(), "todos.md"))
	if err != nil {
		t.Fatal(err)
	}
	a := &app{store: store}
	a.pomodoro = NewPomodoro(store, nil, 40)
	return a, ui.NewTester(a.panelView, panelWidth, 640)
}

func TestPanelFooterStaysVisibleAtMaxHeight(t *testing.T) {
	a, tt := newPanelTestApp(t)
	for i := 0; i < 8; i++ {
		if err := a.store.Add(fmt.Sprintf("任务 %d #work !1", i+1), todo.Today); err != nil {
			t.Fatal(err)
		}
	}
	for _, tier := range []todo.Tier{todo.ThreeDay, todo.Week} {
		if err := a.store.Add("后续任务 #work", tier); err != nil {
			t.Fatal(err)
		}
	}
	a.activeTag = "work"
	a.showHelp = true
	a.setStatus("测试错误状态")
	if err := tt.Click("添加待办"); err != nil {
		t.Fatal(err)
	}
	tt.SetSize(panelWidth, 680)
	for _, label := range []string{"添加待办", "打开文件", "使用帮助", "退出 Hibi"} {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s is missing", label)
		}
		if r.X < 0 || r.Y < 0 || r.X+r.W > panelWidth-2 || r.Y+r.H > 676 {
			t.Errorf("%s is clipped: %+v", label, r)
		}
	}
}

func TestSettingsScrollWithinThePanel(t *testing.T) {
	a, tt := newPanelTestApp(t)
	a.autoSense = defaultAutoSense()
	a.showSettings = true
	tt.SetSize(panelWidth, 680)
	inside := func(label string) bool {
		r, ok := tt.Find(label)
		return ok && r.Y >= 0 && r.Y+r.H <= 676
	}
	if !inside("退出 Hibi") {
		t.Fatalf("the toolbar is clipped")
	}
	tt.Scroll(panelWidth/2, 300, 0, 2000)
	if !inside("简体中文") || !inside("退出 Hibi") {
		t.Fatalf("the language switch is out of reach")
	}
}

func TestPanelQuickAddAndShortcuts(t *testing.T) {
	a, tt := newPanelTestApp(t)
	if !tt.HasText("今天没有待办") {
		t.Fatalf("empty state missing: %q", tt.Texts())
	}
	add := func(text string, mods ui.Modifiers) {
		t.Helper()
		if err := tt.Click("添加待办"); err != nil {
			t.Fatal(err)
		}
		tt.Type(text)
		tt.Key(mods, ui.KeyEnter)
	}
	add("今日任务 #work !1", 0)
	add("三日任务", ui.Alt)
	add("本周任务", ui.Cmd)
	data := a.store.Snapshot()
	if len(data.TodayItems) != 1 || len(data.ThreeDayItems) != 1 || len(data.WeekItems) != 1 {
		t.Fatalf("quick add tiers: %#v", data)
	}
	item := data.TodayItems[0]
	if item.Text != "今日任务" || item.Quadrant != todo.Quadrant1 || len(item.Tags) != 1 {
		t.Fatalf("quick add item=%#v", item)
	}
	if a.quickAdd != "" || !tt.HasText("今日任务") || !tt.HasText("三日任务") || !tt.HasText("本周任务") {
		t.Fatalf("view after add=%q quickAdd=%q", tt.Texts(), a.quickAdd)
	}
}

func TestPanelContextMoveEditAndTagFilter(t *testing.T) {
	a, tt := newPanelTestApp(t)
	if err := a.store.Add("写报告 #work !1", todo.Today); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("添加待办"); err != nil { // redraw after the direct fixture mutation
		t.Fatal(err)
	}
	if err := tt.RightClick("写报告"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("象限", "重要不紧急 !2"); err != nil {
		t.Fatal(err)
	}
	if got := a.store.Snapshot().TodayItems[0].Quadrant; got != todo.Quadrant2 {
		t.Fatalf("quadrant=%v", got)
	}
	if err := tt.RightClick("写报告"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("编辑"); err != nil {
		t.Fatal(err)
	}
	if a.editingID == 0 || !a.editingActive.Load() {
		t.Fatal("edit did not start")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("改后的报告 #next !3")
	tt.Key(0, ui.KeyEnter)
	item := a.store.Snapshot().TodayItems[0]
	if item.Text != "改后的报告" || item.Quadrant != todo.Quadrant3 || len(item.Tags) != 1 || item.Tags[0] != "next" {
		t.Fatalf("edited item=%#v", item)
	}
	if a.editingID != 0 || a.editingActive.Load() {
		t.Fatal("edit did not finish")
	}
	if err := tt.Click("#next"); err != nil {
		t.Fatal(err)
	}
	if a.activeTag != "next" || !tt.HasText("清除标签筛选 #next") {
		t.Fatalf("tag filter=%q texts=%q", a.activeTag, tt.Texts())
	}
	if err := tt.Click("清除标签筛选 #next"); err != nil {
		t.Fatal(err)
	}
	if a.activeTag != "" {
		t.Fatal("filter did not clear")
	}
	if err := tt.RightClick("改后的报告"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移动到", "三日"); err != nil {
		t.Fatal(err)
	}
	data := a.store.Snapshot()
	if len(data.TodayItems) != 0 || len(data.ThreeDayItems) != 1 {
		t.Fatalf("move result=%#v", data)
	}
}

func TestBreakPromptAndIcons(t *testing.T) {
	for name, icon := range map[string]*ui.SVG{
		"play": icons.play, "pause": icons.pause, "stop": icons.stop,
		"plus": icons.plus, "file": icons.file, "help": icons.help,
		"quit": icons.quit, "timer": icons.timer,
		"check": icons.check, "trash": icons.trash, "chevron": icons.chevron,
		"cat": icons.cat, "close": icons.close, "arrow": icons.arrow,
	} {
		if icon == nil {
			t.Fatalf("%s icon is nil", name)
		}
		w, h := icon.Size()
		if w != 24 || h != 24 {
			t.Fatalf("%s icon size = %vx%v", name, w, h)
		}
	}
	a, _ := newPanelTestApp(t)
	a.pomodoro.Start(5)
	tt := ui.NewTester(a.breakPromptView, 320, 260)
	if !tt.HasText("专注结束") || !tt.HasText("开始休息") || !tt.HasText("跳过") {
		t.Fatalf("break prompt texts = %q", tt.Texts())
	}
	a.pomodoro.Stop()
}

func TestBreakPromptSaysTheStreakAndFitsTheTipToIt(t *testing.T) {
	a, _ := newPanelTestApp(t)
	defer a.pomodoro.Stop()
	a.pomodoro.Start(30)
	finishCountdown(a.pomodoro)
	tt := ui.NewTester(a.breakPromptView, 320, 260)
	if tt.HasText("已经连续专注 30 分钟，中间没有休息") || !tt.HasText("上个厕所，倒杯水，走一走。") {
		t.Fatalf("first prompt = %q", tt.Texts())
	}

	a.pomodoro.Start(60)
	finishCountdown(a.pomodoro)
	a.pomodoro.Skip()
	finishCountdown(a.pomodoro)
	tt = ui.NewTester(a.breakPromptView, 320, 260)
	if !tt.HasText("60 分钟，完成。") || !tt.HasText("已经连续专注 150 分钟，中间没有休息") ||
		!tt.HasText("太久没休息了，这次别跳过：去爬几层楼梯，再做一组俯卧撑。") {
		t.Fatalf("prompt after a skipped break = %q", tt.Texts())
	}
}

func TestPanelRendersOverVibrancy(t *testing.T) {
	a, tt := newPanelTestApp(t)
	if err := a.store.Add("在材质上的任务 #work", todo.Today); err != nil {
		t.Fatal(err)
	}
	tt.SetVibrancy(true) // the system popover material shows behind the view
	if err := tt.Click("添加待办"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("在材质上的任务") {
		t.Fatalf("missing task over vibrancy: %q", tt.Texts())
	}
	tt.SetSize(panelWidth, 680)
	for _, label := range []string{"添加待办", "打开文件", "退出 Hibi"} {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s is missing", label)
		}
		if r.X < 0 || r.Y < 0 || r.Y+r.H > 676 {
			t.Errorf("%s is clipped: %+v", label, r)
		}
	}
}

func TestPanelKeyboardDrivesTheList(t *testing.T) {
	a, tt := newPanelTestApp(t)
	for _, text := range []string{"甲", "乙", "丙"} {
		if err := a.store.Add(text, todo.Today); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.store.Add("本周的", todo.Week); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("添加待办"); err != nil {
		t.Fatal(err)
	}
	selected := func() string {
		for _, task := range a.visibleTasks(a.store.Snapshot()) {
			if task.tier == a.selTier && task.item.ID == a.selID {
				return task.item.Text
			}
		}
		return ""
	}
	item := func(text string) todo.Item {
		for _, task := range a.visibleTasks(a.store.Snapshot()) {
			if task.item.Text == text {
				return task.item
			}
		}
		t.Fatalf("no task %q", text)
		return todo.Item{}
	}

	tt.Key(0, ui.KeyDown) // from the field to the first task
	if got := selected(); got != "甲" || !a.listFocused {
		t.Fatalf("down from field selected %q focused=%v", got, a.listFocused)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown) // across into the week section
	if got := selected(); got != "本周的" {
		t.Fatalf("selected %q, want 本周的", got)
	}
	tt.Key(0, ui.KeyDown) // past the end: back to the field
	if a.selID != 0 || !tt.Focused("添加待办") {
		t.Fatalf("past the end selID=%d", a.selID)
	}
	tt.Key(0, ui.KeyUp) // from the field to the last task
	tt.Key(0, ui.KeyUp)
	if got := selected(); got != "丙" {
		t.Fatalf("up selected %q, want 丙", got)
	}

	tt.Key(ui.Cmd, ui.KeyDown) // to the three-day section, still selected
	if a.selTier != todo.ThreeDay || selected() != "丙" || !slices.ContainsFunc(a.store.Snapshot().ThreeDayItems, func(i todo.Item) bool { return i.Text == "丙" }) {
		t.Fatalf("cmd-down: tier=%v selected=%q", a.selTier, selected())
	}
	tt.Key(ui.Cmd, ui.KeyUp) // and back to today
	if a.selTier != todo.Today || selected() != "丙" {
		t.Fatalf("cmd-up: tier=%v selected=%q", a.selTier, selected())
	}
	tt.Key(ui.Cmd, ui.KeyUp) // today is the top section: nothing to do
	if a.selTier != todo.Today || selected() != "丙" {
		t.Fatalf("cmd-up at top: tier=%v selected=%q", a.selTier, selected())
	}

	tt.Key(0, ui.Key1)
	if got := item("丙").Quadrant; got != todo.Quadrant1 || selected() != "丙" {
		t.Fatalf("quadrant=%v selected=%q", got, selected())
	}
	tt.Key(0, ui.KeySpace)
	if !item("丙").Done || selected() != "丙" {
		t.Fatalf("space did not complete 丙, selected %q", selected())
	}
	tt.Key(0, ui.KeySpace)
	tt.Key(0, ui.KeyF)
	if got := a.pomodoro.Snapshot(); got.Phase != phaseFocus || got.BoundTaskText != "丙" {
		t.Fatalf("F did not time 丙: %#v", got)
	}
	a.pomodoro.Stop()

	tt.Key(0, ui.KeyEnter) // edit in place
	if a.editingID != item("丙").ID {
		t.Fatalf("enter did not edit: editingID=%d", a.editingID)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("丙改")
	tt.Key(0, ui.KeyEnter)
	if a.editingID != 0 || selected() != "丙改" || !a.listFocused {
		t.Fatalf("after edit: editing=%d selected=%q focused=%v", a.editingID, selected(), a.listFocused)
	}

	tt.Key(ui.Cmd, ui.KeyBackspace)
	if got := len(a.store.Snapshot().TodayItems); got != 2 || selected() == "" {
		t.Fatalf("delete left %d today, selected %q", got, selected())
	}
	tt.Key(0, ui.KeyEscape)
	if a.selID != 0 || !tt.Focused("添加待办") {
		t.Fatalf("escape selID=%d", a.selID)
	}
}

func TestPanelClickSelectsTheRow(t *testing.T) {
	a, tt := newPanelTestApp(t)
	for _, text := range []string{"第一", "第二"} {
		if err := a.store.Add(text, todo.Today); err != nil {
			t.Fatal(err)
		}
	}
	tt.Frame()
	if err := tt.Click("第二"); err != nil {
		t.Fatal(err)
	}
	if want := a.store.Snapshot().TodayItems[1].ID; a.selID != want || !a.listFocused {
		t.Fatalf("click selected %d focused=%v, want %d", a.selID, a.listFocused, want)
	}
	tt.Key(0, ui.KeyUp)
	if want := a.store.Snapshot().TodayItems[0].ID; a.selID != want {
		t.Fatalf("up after click selected %d, want %d", a.selID, want)
	}
}

func TestPanelSettingsChangeAutoSense(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // settings.json goes here, not into the real one
	a, tt := newPanelTestApp(t)
	defer a.stopPresence()
	a.autoSense = defaultAutoSense()
	a.awayIndex = slices.Index(awayPresets, a.autoSense.AwayMinutes)
	a.sitIndex = slices.Index(sitPresets, a.autoSense.SitMinutes)
	// The title text comes first by that name; its switch ends the row.
	flip := func(title string) {
		t.Helper()
		r, ok := tt.Find(title)
		if !ok {
			t.Fatalf("%s is missing", title)
		}
		tt.ClickAt(panelWidth-24, r.Y+r.H/2)
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("自动感知") || tt.HasText("今天没有待办") {
		t.Fatalf("settings did not replace the list: %q", tt.Texts())
	}
	flip("在电脑前时自动开始专注")
	if err := tt.Click("10"); err != nil {
		t.Fatal(err)
	}
	want := autoSense{AutoStart: false, EndOnAway: true, AwayMinutes: 10, SitReminder: true, SitMinutes: 30, ActivityAware: true}
	if a.autoSense != want {
		t.Fatalf("autoSense=%+v want %+v", a.autoSense, want)
	}
	if got := loadPreferences().autoSense(); got != want {
		t.Fatalf("saved=%+v want %+v", got, want)
	}
	a.pomodoro.UserPresent()
	if got := a.pomodoro.Snapshot(); got.Phase != phaseIdle {
		t.Fatalf("auto-start off in the settings, yet the timer started: %#v", got)
	}

	monitorRuns := func() bool {
		a.presenceMu.Lock()
		defer a.presenceMu.Unlock()
		return a.presence != nil
	}
	flip("离开时结束本轮")
	if !monitorRuns() || !tt.HasText("上面两项都关闭时，用不到这一项。") {
		t.Fatalf("the sitting reminder still needs the monitor: running=%v texts=%q", monitorRuns(), tt.Texts())
	}
	if err := tt.Click("45"); err != nil {
		t.Fatal(err)
	}
	if got := loadPreferences().autoSense().SitMinutes; got != 45 {
		t.Fatalf("saved sitting minutes=%d", got)
	}
	flip("久坐提醒")
	if monitorRuns() || tt.HasText("全部关闭时，Hibi 不读取键盘和鼠标的空闲时间。") {
		t.Fatalf("activity on: monitor running=%v texts=%q", monitorRuns(), tt.Texts())
	}
	flip("识别视频、会议和打字")
	if a.activityAware.Load() || loadPreferences().autoSense().ActivityAware {
		t.Fatal("the activity switch did not turn it off")
	}
	if monitorRuns() || !tt.HasText("全部关闭时，Hibi 不读取键盘和鼠标的空闲时间。") {
		t.Fatalf("all off: monitor running=%v texts=%q", monitorRuns(), tt.Texts())
	}

	if err := tt.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("今天没有待办") {
		t.Fatalf("closing the settings did not bring the list back: %q", tt.Texts())
	}
}

// Editing a task keeps its text where it was, wrapped the same way and no
// taller, so the rows below do not move; Enter saves, and a line break in
// the text becomes a space.
func TestPanelEditKeepsTheLayout(t *testing.T) {
	a, tt := newPanelTestApp(t)
	long := "allsum 部署依赖，文档记录，sg staging 集群，sg prod 集群，kafka issues,flink msf"
	for _, text := range []string{"一行的任务", "最后一项", long, "下一项任务"} {
		if err := a.store.Add(text, todo.Today); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("添加待办"); err != nil {
		t.Fatal(err)
	}
	edit := func(text, next string) {
		t.Helper()
		before, _ := tt.Find(next)
		if err := tt.RightClick(text); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("编辑"); err != nil {
			t.Fatal(err)
		}
		if a.editingID == 0 {
			t.Fatal("edit did not start")
		}
		if during, _ := tt.Find(next); during != before {
			t.Fatalf("editing %q moved the next row from %v to %v", text, before, during)
		}
	}
	edit("一行的任务", "最后一项")
	tt.Key(0, ui.KeyEscape)
	edit(long, "下一项任务")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("第一行\n第二行")
	if a.editingID == 0 {
		t.Fatal("Enter in the typed text ended the edit")
	}
	tt.Key(0, ui.KeyEnter)
	if a.editingID != 0 {
		t.Fatal("Enter did not save")
	}
	if got := todayTexts(a)[2]; got != "第一行 第二行" {
		t.Fatalf("saved %q", got)
	}
	if err := tt.RightClick("第一行 第二行"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("编辑"); err != nil {
		t.Fatal(err)
	}
	tt.Type("不要")
	tt.Key(0, ui.KeyEscape)
	if a.editingID != 0 || todayTexts(a)[2] != "第一行 第二行" {
		t.Fatalf("Escape did not cancel: editing=%d text=%q", a.editingID, todayTexts(a)[2])
	}
}

func todayTexts(a *app) []string {
	var texts []string
	for _, item := range a.store.Snapshot().TodayItems {
		texts = append(texts, item.Text)
	}
	return texts
}

// The header keeps its height in every timer state: a header that changed
// height would resize the panel, which macOS animates.
func TestPanelHeaderKeepsItsHeight(t *testing.T) {
	a, tt := newPanelTestApp(t)
	if err := a.store.Add("第一项待办", todo.Today); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("添加待办"); err != nil {
		t.Fatal(err)
	}
	firstRow := func() float32 {
		tt.Frame()
		r, ok := tt.Find("第一项待办")
		if !ok {
			t.Fatal("the task is missing")
		}
		return r.Y
	}
	want := firstRow()
	for _, step := range []struct {
		state string
		do    func()
	}{
		{"focus", func() { a.pomodoro.Start(30) }},
		{"paused", a.pomodoro.Pause},
		{"resumed", a.pomodoro.Resume},
		{"break pending", func() { finishCountdown(a.pomodoro) }},
		{"break", a.pomodoro.StartBreak},
		{"idle", a.pomodoro.Stop},
	} {
		step.do()
		if got := firstRow(); got != want {
			t.Errorf("%s: the list starts at %v, not %v", step.state, got, want)
		}
	}
}
