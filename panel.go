package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) panelView(c *ui.Context) {
	p := applyPalette(c)
	paper := paperColor(c, p)
	c.Root().Background(paper)
	ui.Column(c).Fill().Width(panelWidth).Background(paper).Children(func() {
		data := a.store.Snapshot()
		timer := a.pomodoro.Snapshot()
		tasks := a.visibleTasks(data)
		a.listFocused = a.listHandle.FocusWithin(c)
		a.headerView(c, p, data, timer)
		rule(c, p, 1)
		if a.showSettings {
			a.settingsView(c, p)
			a.toolbarView(c, p)
			return
		}
		ui.Scroll(c).Grow(1).MinHeight(0).MaxHeight(420).Children(func() {
			// The list takes the keyboard from the quick-add field's arrows
			// (listKeys); out of Tab's way, and the selected row shows it.
			list := ui.Column(c.Key("tasks")).Padding(8).Gap(10).Focusable().TabStop(false).FocusRing(false).Bind(&a.listHandle)
			list.Children(func() {
				a.sectionsView(c, p, data, timer)
				if a.showHelp {
					a.helpView(c, p)
				}
			})
			a.listKeys(c, list, tasks)
		})
		a.quickAddView(c, p, tasks)
		if status := a.statusText(); status != "" {
			ui.Row(c).Padding(7, 12).Background(p.Surface).Children(func() {
				ui.Text(c, status).FontSize(11).TextColor(p.Muted).MaxLines(2).Ellipsis("…")
			})
		}
		a.toolbarView(c, p)
	})
}

// timerRowHeight holds the tallest of the timer's controls: the break
// prompt's text button.
const timerRowHeight = 19

func (a *app) headerView(c *ui.Context, p palette, data todo.Data, timer pomodoroSnapshot) {
	ui.Column(c).Padding(11, 14, 9, 14).Gap(7).Children(func() {
		ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "今日").FontSize(13).FontWeight(600).TextColor(p.Ink)
			ui.Text(c, todo.HeaderDisplay(data.TodayDate)).FontSize(11).TextColor(p.Muted)
			ui.Spacer(c)
			_, done, total := todo.TodayCounts(data, a.activeTag)
			ui.Text(c, fmt.Sprintf("%d/%d", done, total)).Font("Menlo").FontSize(11).TextColor(p.Muted)
		})
		// A fixed height: each timer state has other buttons, and a header
		// that changes height resizes the panel, which macOS animates.
		ui.Row(c).FillWidth().Height(timerRowHeight).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Tooltip("今天累计的专注时长，含没有绑定任务的轮次").Children(func() {
				ui.Text(c, "专注").FontSize(11).TextColor(p.Muted)
				ui.Text(c, formatFocus(a.todayFocusSeconds(timer))).Font("Menlo").FontSize(11).TextColor(p.Muted)
			})
			if a.activeTag != "" {
				tag := a.activeTag
				b := ui.ButtonBase(c).Label("清除标签筛选 #"+tag).Padding(3, 7).Background(p.Ink).Radius(4)
				b.Children(func() {
					ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "#"+tag).FontSize(11).FontWeight(500).TextColor(p.Paper)
						ui.Icon(c, icons.close).Size(9, 9).TextColor(p.Paper)
					})
				})
				if b.Clicked() {
					a.activeTag = ""
				}
			}
			ui.Spacer(c)
			a.pomodoroView(c, p, timer)
		})
	})
}

func (a *app) pomodoroView(c *ui.Context, p palette, timer pomodoroSnapshot) {
	switch timer.Phase {
	case phaseIdle:
		b := ui.ButtonBase(c).Label("开始专注").Tooltip("选择专注时长").Padding(3, 4).Radius(4)
		if b.Hovered() || b.Pressed() {
			b.Background(p.Surface)
		}
		b.Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				glyph(c, "play.fill", icons.play, 11, p.Muted)
				ui.Text(c, strconv.Itoa(timer.SelectedFocusMinutes)+"m").Font("Menlo").FontSize(10).TextColor(p.Muted)
				glyph(c, "chevron.down", icons.chevron, 9, p.Muted)
			})
		})
		b.Menu(func(m *ui.Menu) {
			for _, minutes := range focusPresets {
				if m.Item(fmt.Sprintf("%d 分钟", minutes)).Chosen() {
					a.pomodoro.Start(minutes)
				}
			}
		})
	case phaseFocus, phasePaused:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if timer.BoundTaskText != "" {
				ui.Text(c, truncateRunes(timer.BoundTaskText, 8)).MaxLines(1).Ellipsis("…").FontSize(10).TextColor(p.Muted)
			}
			ui.Text(c, timer.TimeString()).Font("Menlo").FontSize(11).TextColor(p.Ink)
			if timer.Phase == phaseFocus {
				if iconButton(c, "pause.fill", icons.pause, "暂停", p).Clicked() {
					a.pomodoro.Pause()
				}
			} else if iconButton(c, "play.fill", icons.play, "继续", p).Clicked() {
				a.pomodoro.Resume()
			}
			if iconButton(c, "stop.fill", icons.stop, "停止并结算", p).Clicked() {
				a.pomodoro.Stop()
			}
		})
	case phaseBreakPending:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			glyph(c, "", icons.cat, 14, p.Muted)
			ui.Text(c, "该休息了").FontSize(10).FontWeight(600)
			if textButton(c, "", nil, "开始", "开始休息", p).Clicked() {
				a.pomodoro.StartBreak()
			}
			if iconButton(c, "arrow.right", icons.arrow, "跳过休息", p).Clicked() {
				a.pomodoro.Skip()
			}
		})
	case phaseBreak:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			glyph(c, "", icons.catAsleep, 14, p.Muted)
			ui.Text(c, timer.TimeString()).Font("Menlo").FontSize(11).TextColor(p.Muted)
			if iconButton(c, "arrow.right", icons.arrow, "跳过休息", p).Clicked() {
				a.pomodoro.Skip()
			}
		})
	}
}

func (a *app) sectionsView(c *ui.Context, p palette, data todo.Data, timer pomodoroSnapshot) {
	if a.activeTag == "" {
		if len(data.TodayItems) == 0 {
			emptyState(c, p, "今天没有待办")
		} else {
			a.sectionView(c, p, data, timer, todo.Today, "")
		}
		if len(data.ThreeDayItems) > 0 {
			sectionHeading(c, p, "三日")
			a.sectionView(c, p, data, timer, todo.ThreeDay, "")
		}
		if len(data.WeekItems) > 0 {
			sectionHeading(c, p, "本周")
			a.sectionView(c, p, data, timer, todo.Week, "")
		}
		return
	}
	matches := 0
	for _, tier := range []todo.Tier{todo.Today, todo.ThreeDay, todo.Week} {
		items := todo.FilteredItems(data, tier, a.activeTag)
		if len(items) == 0 {
			continue
		}
		sectionHeading(c, p, tier.String())
		a.sectionView(c, p, data, timer, tier, "")
		matches += len(items)
	}
	if matches == 0 {
		emptyState(c, p, "没有匹配的任务")
	}
}

func sectionHeading(c *ui.Context, p palette, title string) {
	ui.Text(c, title).FontSize(10.5).FontWeight(600).TextColor(p.Muted).Padding(2, 10, 3, 10)
}

func (a *app) sectionView(c *ui.Context, p palette, data todo.Data, timer pomodoroSnapshot, tier todo.Tier, title string) {
	ui.Column(c).Gap(1).Children(func() {
		if title != "" {
			sectionHeading(c, p, title)
		}
		for _, item := range todo.FilteredItems(data, tier, a.activeTag) {
			a.taskRow(c, p, item, tier, timer)
		}
	})
}

func (a *app) taskRow(c *ui.Context, p palette, item todo.Item, tier todo.Tier, timer pomodoroSnapshot) {
	row := ui.Row(c.Key(fmt.Sprintf("task-%d-%d", tier, item.ID))).Gap(7).AlignItems(ui.Center).Padding(5, 8, 5, 10)
	selected := a.listFocused && a.selID == item.ID && a.selTier == tier
	if selected && a.selScroll {
		row.ScrollIntoView()
		a.selScroll = false
	}
	row = taskCard(c, row, p, row.Hovered(), selected)
	row.Children(func() {
		if !item.Done {
			ui.Box(c).Width(2).Height(24).Background(quadrantColor(int(item.Quadrant), p)).Radius(1)
		} else {
			ui.Box(c).Width(2).Height(24)
		}
		check := ui.ButtonBase(c).Size(22, 22).Label("切换 " + item.Text).Tooltip("完成 / 取消完成").Padding(4).Radius(4)
		check.Children(func() {
			if item.Done {
				ui.Icon(c, icons.check).Size(12, 12).TextColor(p.Muted)
			} else {
				ui.Box(c).Size(13, 13).Radius(7).Border(1.2, p.Faint)
			}
		})
		if check.Clicked() {
			a.perform(a.store.Toggle(item.ID, tier))
		}
		if a.editingID == item.ID && a.editingTier == tier {
			// A text area without a look, at the task text's size: it wraps
			// as the text did, so the row keeps its height and the text its
			// place. Enter saves rather than breaking the line.
			input := ui.TextAreaBase(c.Key("edit"), &a.editText).Grow(1).Lines(1, 12).FontSize(13).TextColor(p.Ink).
				Bind(&a.editHandle).AutoFocus().Label("编辑任务").
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key != ui.KeyEnter {
						return false
					}
					a.editSubmit = true
					return true
				})
			ui.Box(c).Width(38) // the actions' column, empty while editing
			if a.editSubmit {
				a.editSubmit = false
				a.finishEdit(c, true)
			} else if input.Shortcut(0, ui.KeyEscape) {
				a.finishEdit(c, false)
			} else if a.editingID == item.ID {
				focused := a.editHandle.Focused(c)
				if a.editStarting {
					a.editStarting = false
				} else if a.editFocused && !focused {
					a.finishEdit(c, true)
				}
				a.editFocused = focused
			}
			return
		}

		ui.Row(c).Grow(1).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
			textColor := p.Ink
			if item.Done {
				textColor = p.Muted
			}
			text := ui.Text(c, item.Text).FontSize(13).TextColor(textColor)
			if item.Done {
				text.Strikethrough()
			}
			if len(item.Tags) > 0 || item.FocusMinutes > 0 || item.Quadrant != todo.QuadrantNone {
				ui.Row(c).Gap(5).Wrap().AlignItems(ui.Center).Children(func() {
					for _, tag := range item.Tags {
						b := ui.ButtonBase(c).Label("筛选 #" + tag)
						b.Children(func() { ui.Text(c, "#"+tag).FontSize(11).TextColor(p.Muted) })
						if b.Clicked() {
							if a.activeTag == tag {
								a.activeTag = ""
							} else {
								a.activeTag = tag
							}
						}
					}
					if item.FocusMinutes > 0 {
						ui.Text(c, fmt.Sprintf("%dm", item.FocusMinutes)).Font("Menlo").FontSize(11).TextColor(p.Muted)
					}
				})
			}
		})

		running := timer.Phase == phaseFocus || timer.Phase == phasePaused
		bound := running && timer.BoundTaskID == item.ID && timer.BoundTaskTier == tier
		playLabel := "开始专注"
		if running {
			playLabel = "改为给此任务计时，倒计时继续"
		}
		ui.Row(c).Width(38).Gap(4).AlignItems(ui.Center).Children(func() {
			if row.Hovered() || bound || selected {
				if bound {
					glyph(c, "timer", icons.timer, 12, p.Ink).Label("正在为此任务计时")
				} else if iconButton(c, "play.fill", icons.play, playLabel, p).Disabled(item.Done).Clicked() {
					a.pomodoro.StartTask(item, tier, 0)
				}
				if iconButton(c, "trash", icons.trash, "删除任务", p).Clicked() {
					a.perform(a.store.Delete(item.ID, tier))
				}
			}
		})
	})
	if row.Clicked() && a.editingID == 0 {
		// A click on the list focuses it; select the row, so the keys that
		// follow act on it rather than on nothing.
		a.selectTask(c, taskRef{tier, item})
	}
	if row.DoubleClicked() {
		a.beginEdit(c, item, tier)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("编辑").Chosen() {
			a.beginEdit(c, item, tier)
		}
		m.Submenu("开始专注", func(sub *ui.Menu) {
			for _, minutes := range focusPresets {
				if sub.Item(fmt.Sprintf("%d 分钟", minutes)).Disabled(item.Done).Chosen() {
					a.pomodoro.StartTask(item, tier, minutes)
				}
			}
		})
		m.Submenu("象限", func(sub *ui.Menu) {
			labels := []string{"重要且紧急 !1", "重要不紧急 !2", "紧急不重要 !3", "不重要不紧急 !4"}
			for i, label := range labels {
				quadrant := todo.Quadrant(i + 1)
				if sub.Item(label).Radio(item.Quadrant == quadrant).Chosen() {
					a.perform(a.store.SetQuadrant(item.ID, quadrant, tier))
				}
			}
			if sub.Item("清除标记").Chosen() {
				a.perform(a.store.SetQuadrant(item.ID, todo.QuadrantNone, tier))
			}
		})
		m.Submenu("移动到", func(sub *ui.Menu) {
			for _, target := range []todo.Tier{todo.Today, todo.ThreeDay, todo.Week} {
				if target != tier && sub.Item(target.String()).Chosen() {
					a.perform(a.store.Move(item.ID, tier, target))
				}
			}
		})
		m.Separator()
		if m.Item("删除").Danger().Chosen() {
			a.perform(a.store.Delete(item.ID, tier))
		}
	})
}

func emptyState(c *ui.Context, p palette, text string) {
	ui.Column(c).Padding(16, 12).Center().Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(p.Muted)
	})
}

// taskRef is one task as the panel lists it.
type taskRef struct {
	tier todo.Tier
	item todo.Item
}

// visibleTasks lists the tasks the panel shows, in its order, which is the
// order the arrow keys move through.
func (a *app) visibleTasks(data todo.Data) []taskRef {
	var tasks []taskRef
	for _, tier := range []todo.Tier{todo.Today, todo.ThreeDay, todo.Week} {
		for _, item := range todo.FilteredItems(data, tier, a.activeTag) {
			tasks = append(tasks, taskRef{tier, item})
		}
	}
	return tasks
}

func (a *app) selectTask(c *ui.Context, t taskRef) {
	a.selTier, a.selID, a.selScroll = t.tier, t.item.ID, true
	a.listHandle.Focus(c)
}

func (a *app) clearSelection() { a.selID = 0 }

func (a *app) focusQuickAdd(c *ui.Context) {
	a.clearSelection()
	a.quickAddHandle.Focus(c)
}

// listKeys handles the keys of the focused task list: the arrows move the
// selection, and the other keys act on the selected task. The arrows of
// the quick-add field start it (quickAddView).
func (a *app) listKeys(c *ui.Context, list ui.Element, tasks []taskRef) {
	if a.editingID != 0 || len(tasks) == 0 {
		return
	}
	i := slices.IndexFunc(tasks, func(t taskRef) bool { return t.tier == a.selTier && t.item.ID == a.selID })
	if i < 0 {
		// Nothing selected yet (a click focused the list), or the selected
		// task went away (an external edit).
		switch {
		case list.Shortcut(0, ui.KeyDown):
			a.selectTask(c, tasks[0])
		case list.Shortcut(0, ui.KeyUp):
			a.selectTask(c, tasks[len(tasks)-1])
		case list.Shortcut(0, ui.KeyEscape):
			a.focusQuickAdd(c)
		}
		return
	}
	t := tasks[i]
	switch {
	case list.Shortcut(ui.Cmd, ui.KeyUp):
		a.moveTask(t, -1)
	case list.Shortcut(ui.Cmd, ui.KeyDown):
		a.moveTask(t, 1)
	case list.Shortcut(0, ui.KeyUp):
		if i > 0 {
			a.selectTask(c, tasks[i-1])
		}
	case list.Shortcut(0, ui.KeyDown):
		if i+1 < len(tasks) {
			a.selectTask(c, tasks[i+1])
		} else {
			a.focusQuickAdd(c) // the field is below the list
		}
	case list.Shortcut(0, ui.KeyEscape):
		a.focusQuickAdd(c)
	case list.Shortcut(0, ui.KeySpace):
		a.perform(a.store.Toggle(t.item.ID, t.tier))
		a.selScroll = true // done tasks sort last; the selection follows
	case list.Shortcut(0, ui.KeyEnter):
		a.beginEdit(c, t.item, t.tier)
	case list.Shortcut(0, ui.KeyF):
		if !t.item.Done {
			a.pomodoro.StartTask(t.item, t.tier, 0)
		}
	case list.Shortcut(ui.Cmd, ui.KeyBackspace):
		a.perform(a.store.Delete(t.item.ID, t.tier))
		rest := slices.Delete(slices.Clone(tasks), i, i+1)
		if len(rest) == 0 {
			a.focusQuickAdd(c)
		} else {
			a.selectTask(c, rest[min(i, len(rest)-1)])
		}
	default:
		for q, key := range []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4} {
			if list.Shortcut(0, key) {
				a.perform(a.store.SetQuadrant(t.item.ID, todo.Quadrant(q), t.tier))
				a.selScroll = true
			}
		}
	}
}

// moveTask moves a task to the section above (step -1) or below (step 1),
// in the order the panel shows them. The selection goes with it.
func (a *app) moveTask(t taskRef, step int) {
	tiers := []todo.Tier{todo.Today, todo.ThreeDay, todo.Week}
	j := slices.Index(tiers, t.tier) + step
	if j < 0 || j >= len(tiers) {
		return
	}
	a.perform(a.store.Move(t.item.ID, t.tier, tiers[j]))
	a.selTier, a.selScroll = tiers[j], true
}

func (a *app) quickAddView(c *ui.Context, p palette, tasks []taskRef) {
	rule(c, p, 1)
	// Lined up as the next task row: the plus under the rows' check circles,
	// the text where their text starts (taskRow: 8+10+2+7 to the circle's
	// 22pt box, 7 more to the text).
	ui.Row(c).Padding(10, 14, 10, 32).Gap(12).AlignItems(ui.Center).Background(paperColor(c, p)).Children(func() {
		glyph(c, "plus", icons.plus, 12, p.Faint)
		placeholder := "添加待办…"
		if a.activeTag != "" {
			placeholder = "添加到 #" + a.activeTag + "…"
		}
		input := ui.TextInput(c.Key("quick-add"), &a.quickAdd).Grow(1).Bind(&a.quickAddHandle).Label("添加待办").Placeholder(placeholder).Background(paperColor(c, p)).Border(0, p.Paper).Radius(0).FontSize(13).Padding(6, 0)
		switch {
		case input.Shortcut(0, ui.KeyUp) && len(tasks) > 0:
			a.selectTask(c, tasks[len(tasks)-1]) // the list is above the field
		case input.Shortcut(0, ui.KeyDown) && len(tasks) > 0:
			a.selectTask(c, tasks[0])
		case input.Shortcut(ui.Alt, ui.KeyEnter):
			a.submitQuickAdd(c, todo.ThreeDay)
		case input.Shortcut(ui.Cmd, ui.KeyEnter):
			a.submitQuickAdd(c, todo.Week)
		case input.Submitted():
			a.submitQuickAdd(c, todo.Today)
		}
		if iconButton(c, "arrow.right", icons.arrow, "添加到今日", p).Disabled(strings.TrimSpace(a.quickAdd) == "").Clicked() {
			a.submitQuickAdd(c, todo.Today)
		}
	})
}

func (a *app) submitQuickAdd(c *ui.Context, tier todo.Tier) {
	if strings.TrimSpace(a.quickAdd) == "" {
		return
	}
	raw := a.quickAdd
	if a.activeTag != "" {
		raw += " #" + a.activeTag
	}
	if err := a.store.Add(raw, tier); err != nil {
		a.perform(err)
		return
	}
	a.quickAdd = ""
	a.perform(nil)
	a.quickAddHandle.Focus(c)
}

func (a *app) toolbarView(c *ui.Context, p palette) {
	rule(c, p, 1)
	ui.Row(c).Padding(9, 14).Gap(8).AlignItems(ui.Center).Background(paperColor(c, p)).Children(func() {
		open := textButton(c, "doc.text", icons.file, "todos.md", "打开文件", p)
		if open.Clicked() {
			a.openTodoFile()
		}
		open.ContextMenu(func(m *ui.Menu) {
			if m.Item("在 Finder / 文件管理器中显示").Chosen() {
				mygo.Shell.ShowItemInFolder(a.store.Path())
			}
			if m.Item("拷贝路径").Chosen() {
				mygo.Clipboard.WriteText(a.store.Path())
			}
		})
		ui.Spacer(c)
		if iconButton(c, "slider.horizontal.3", icons.sliders, "设置", p).Clicked() {
			a.showSettings = !a.showSettings
		}
		if iconButton(c, "questionmark.circle", icons.help, "使用帮助", p).Clicked() {
			a.showHelp = !a.showHelp
			a.showSettings = false
		}
		if textButton(c, "power", icons.quit, "退出", "退出 Hibi", p).Clicked() {
			mygo.App.Quit()
		}
	})
}

func (a *app) helpView(c *ui.Context, p palette) {
	ui.Column(c).Padding(12).Gap(9).Background(paperColor(c, p)).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			glyph(c, "questionmark.circle", icons.help, 13, p.Muted)
			ui.Text(c, "Hibi 使用说明").FontSize(11.5).FontWeight(600).TextColor(p.Ink)
		})
		for _, line := range []string{
			"⌃⌥H 在任何应用里打开 Hibi；Esc 关闭",
			"↩ 今日 · ⌥↩ 三日 · ⌘↩ 本周",
			"↑↓ 选任务：空格完成 · ↩ 编辑 · F 计时 · 1~4 象限 · ⌘⌫ 删除",
			"⌘↑↓ 把选中的任务移到上一档或下一档",
			"正文 #标签 !1~!4；支持 ⏱40m 时长标记",
			"双击编辑，回车保存，Esc 放弃；右键更多操作",
			"点击标签筛选；筛选时新增任务自动带上该标签",
			"自动开始专注、离开时结束本轮、久坐提醒，都可在设置里调整",
			"悬停任务点播放按钮，把番茄钟记到该任务",
			"所有修改实时写入 ~/todos.md，外部编辑自动重载",
		} {
			ui.Text(c, line).FontSize(11).TextColor(p.Muted)
		}
	})
}

// settingsView replaces the task list while the settings are open. Each
// change applies and saves at once.
func (a *app) settingsView(c *ui.Context, p palette) {
	ui.Column(c).Padding(12, 14, 14, 14).Gap(12).Background(paperColor(c, p)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "设置").FontSize(13).FontWeight(600).TextColor(p.Ink)
			ui.Spacer(c)
			if iconButton(c, "xmark", icons.close, "关闭设置", p).Clicked() {
				a.showSettings = false
			}
		})
		ui.Text(c, "自动感知").FontSize(10.5).FontWeight(600).TextColor(p.Muted)
		changed := false
		settingRow(c, p, "在电脑前时自动开始专注",
			"启动时、离开后回来时、休息结束时，用上次选的时长开始。点停止后，要等你离开再回来才会再自动开始。",
			func() {
				changed = ui.Switch(c, &a.autoSense.AutoStart).Label("在电脑前时自动开始专注").Changed() || changed
			})
		settingRow(c, p, "离开时结束本轮",
			"离开算作休息，任务只记离开前的时间；正在等待的休息提示也会关闭。",
			func() {
				changed = ui.Switch(c, &a.autoSense.EndOnAway).Label("离开时结束本轮").Changed() || changed
			})
		watchingAway := a.autoSense.watchesAway()
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, "多久没有操作算离开（分钟）").FontSize(12).TextColor(p.Ink)
			ui.Row(c).Children(func() { // the track only as wide as its segments
				if ui.Segmented(c, &a.awayIndex, minuteLabels(awayPresets)...).Label("多久没有操作算离开（分钟）").Disabled(!watchingAway).Changed() {
					a.autoSense.AwayMinutes = awayPresets[a.awayIndex]
					changed = true
				}
			})
			note := "这么久没有键盘或鼠标输入，或电脑睡眠这么久，算作离开。只看视频或读文档不碰电脑，也会算作离开。"
			if a.autoSense.ActivityAware {
				note = "这么久没有键盘或鼠标输入，或电脑睡眠这么久，算作离开。播放视频或开会时不算；只读文档不碰电脑，会算作离开。"
			}
			if !watchingAway {
				note = "上面两项都关闭时，用不到这一项。"
			}
			ui.Text(c, note).FontSize(11).TextColor(p.Muted)
		})
		rule(c, p, 1)
		settingRow(c, p, "久坐提醒",
			"连续坐够下面的时间，发一条通知提醒你起来走 3 分钟，倒计时不停。3 分钟没有操作就算起来过，重新计时。",
			func() {
				changed = ui.Switch(c, &a.autoSense.SitReminder).Label("久坐提醒").Changed() || changed
			})
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, "连续坐多久提醒（分钟）").FontSize(12).TextColor(p.Ink)
			ui.Row(c).Children(func() { // the track only as wide as its segments
				if ui.Segmented(c, &a.sitIndex, minuteLabels(sitPresets)...).Label("连续坐多久提醒（分钟）").Disabled(!a.autoSense.SitReminder).Changed() {
					a.autoSense.SitMinutes = sitPresets[a.sitIndex]
					changed = true
				}
			})
			ui.Text(c, "研究里每 30 分钟起来走 5 分钟，血糖和血压都有改善；每 60 分钟一次，血糖没有改善。").FontSize(11).TextColor(p.Muted)
		})
		rule(c, p, 1)
		settingRow(c, p, "识别视频、会议和打字",
			"播放视频或开会时，不碰电脑也不算离开或起来过。专注结束时你在打字，等你停下再提醒（最多 3 分钟）；在开会，等会议结束（最多 1 小时）。只读系统状态，不截屏，不读内容。",
			func() {
				changed = ui.Switch(c, &a.autoSense.ActivityAware).Label("识别视频、会议和打字").Changed() || changed
			})
		if !a.autoSense.watches() && !a.autoSense.ActivityAware {
			ui.Text(c, "全部关闭时，Hibi 不读取键盘和鼠标的空闲时间。").FontSize(11).TextColor(p.Muted)
		}
		if changed {
			a.setAutoSense(a.autoSense)
		}
	})
}

func minuteLabels(presets []int) []string {
	labels := make([]string, len(presets))
	for i, minutes := range presets {
		labels[i] = strconv.Itoa(minutes)
	}
	return labels
}

// settingRow is a setting's title and switch on one line, its note below.
func settingRow(c *ui.Context, p palette, title, note string, control func()) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(p.Ink).Grow(1)
			control()
		})
		ui.Text(c, note).FontSize(11).TextColor(p.Muted)
	})
}

func (a *app) beginEdit(c *ui.Context, item todo.Item, tier todo.Tier) {
	if a.editingID != 0 && (a.editingID != item.ID || a.editingTier != tier) {
		a.finishEdit(c, true)
	}
	a.editingID, a.editingTier = item.ID, tier
	a.editText = item.EditableText()
	a.editFocused = false
	a.editStarting = true
	a.editSubmit = false
	a.editingActive.Store(true)
}

func (a *app) finishEdit(c *ui.Context, save bool) {
	if a.editingID == 0 {
		return
	}
	// A task is one line in todos.md: a pasted line break becomes a space.
	id, tier, raw := a.editingID, a.editingTier, strings.Join(strings.Fields(strings.ReplaceAll(a.editText, "\n", " ")), " ")
	a.editingID = 0
	a.editFocused = false
	a.editingActive.Store(false)
	if save {
		changed, err := a.store.UpdateText(id, raw, tier)
		if err != nil {
			a.perform(err)
		} else if !changed {
			a.setStatus("任务正文不能为空，已保留原内容")
		}
	}
	if a.reloadPending.Swap(false) {
		a.reloadFromDisk()
	}
	if a.selID != 0 {
		a.listHandle.Focus(c) // back to the list the edit started from
	}
}

func (a *app) breakPromptView(c *ui.Context) {
	p := applyPalette(c)
	timer := a.pomodoro.Snapshot()
	ui.Column(c).Fill().Background(p.Paper).Children(func() {
		ui.Column(c).Padding(16).Center().Gap(9).Background(p.Paper).Children(func() {
			glyph(c, "", icons.cat, 26, p.Muted)
			ui.Text(c, "专注结束").FontSize(16).FontWeight(600).TextColor(p.Ink)
		})
		rule(c, p, 1)
		ui.Column(c).Fill().Center().Padding(20).Gap(15).Children(func() {
			ui.Text(c, fmt.Sprintf("%d 分钟，完成。", timer.SessionFocusMinutes)).Font("Menlo").FontSize(14).TextColor(p.Ink)
			ui.Text(c, fmt.Sprintf("起身活动一下，休息 %d 分钟", breakMinutes)).FontSize(12).TextColor(p.Muted)
			ui.Row(c).Gap(12).Children(func() {
				if textButton(c, "arrow.right", icons.arrow, "跳过", "跳过休息", p).Clicked() {
					a.pomodoro.Skip()
				}
				if textButton(c, "", icons.catAsleep, "开始休息", "开始休息", p).Clicked() {
					a.pomodoro.StartBreak()
				}
			})
		})
	})
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
