package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
	"github.com/UncertaintyDeterminesYou4ndMe/hibi/locales"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

func (a *app) panelView(c *ui.Context) {
	if !c.WindowHidden() {
		a.panelDrawn.Store(true) // drawWhenShown
	}
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
			ui.Text(c, locales.TierToday()).FontSize(13).FontWeight(600).TextColor(p.Ink)
			ui.Text(c, headerDate(c.Locale(), data.TodayDate)).FontSize(11).TextColor(p.Muted)
			ui.Spacer(c)
			_, done, total := todo.TodayCounts(data, a.activeTag)
			ui.Text(c, fmt.Sprintf("%d/%d", done, total)).Font("Menlo").FontSize(11).TextColor(p.Muted)
		})
		// A fixed height: each timer state has other buttons, and a header
		// that changes height resizes the panel, which macOS animates.
		ui.Row(c).FillWidth().Height(timerRowHeight).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Tooltip(locales.FocusTotalTip()).Children(func() {
				ui.Text(c, locales.FocusTotal()).FontSize(11).TextColor(p.Muted)
				ui.Text(c, formatFocus(a.todayFocusSeconds(timer))).Font("Menlo").FontSize(11).TextColor(p.Muted)
			})
			if a.activeTag != "" {
				tag := a.activeTag
				b := ui.ButtonBase(c).Label(locales.ClearTagFilter(tag)).Padding(3, 7).Background(p.Ink).Radius(4)
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
		b := ui.ButtonBase(c).Label(locales.StartFocus()).Tooltip(locales.ChooseFocusLength()).Padding(3, 4).Radius(4)
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
				if m.Item(locales.Minutes(minutes)).Chosen() {
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
				if iconButton(c, "pause.fill", icons.pause, locales.Pause(), p).Clicked() {
					a.pomodoro.Pause()
				}
			} else if iconButton(c, "play.fill", icons.play, locales.Resume(), p).Clicked() {
				a.pomodoro.Resume()
			}
			if iconButton(c, "stop.fill", icons.stop, locales.StopAndRecord(), p).Clicked() {
				a.pomodoro.Stop()
			}
		})
	case phaseBreakPending:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			glyph(c, "", icons.cat, 14, p.Muted)
			ui.Text(c, locales.BreakDue()).FontSize(10).FontWeight(600)
			if textButton(c, "", nil, locales.Start(), locales.StartBreak(), p).Clicked() {
				a.pomodoro.StartBreak()
			}
			if iconButton(c, "arrow.right", icons.arrow, locales.SkipBreak(), p).Clicked() {
				a.pomodoro.Skip()
			}
		})
	case phaseBreak:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			glyph(c, "", icons.catAsleep, 14, p.Muted)
			ui.Text(c, timer.TimeString()).Font("Menlo").FontSize(11).TextColor(p.Muted)
			if iconButton(c, "arrow.right", icons.arrow, locales.SkipBreak(), p).Clicked() {
				a.pomodoro.Skip()
			}
		})
	}
}

func (a *app) sectionsView(c *ui.Context, p palette, data todo.Data, timer pomodoroSnapshot) {
	if a.activeTag == "" {
		if len(data.TodayItems) == 0 {
			emptyState(c, p, locales.EmptyToday())
		} else {
			a.sectionView(c, p, data, timer, todo.Today, "")
		}
		if len(data.ThreeDayItems) > 0 {
			sectionHeading(c, p, tierName(todo.ThreeDay))
			a.sectionView(c, p, data, timer, todo.ThreeDay, "")
		}
		if len(data.WeekItems) > 0 {
			sectionHeading(c, p, tierName(todo.Week))
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
		sectionHeading(c, p, tierName(tier))
		a.sectionView(c, p, data, timer, tier, "")
		matches += len(items)
	}
	if matches == 0 {
		emptyState(c, p, locales.EmptyFilter())
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
		check := ui.ButtonBase(c).Size(22, 22).Label(locales.ToggleTask(item.Text)).Tooltip(locales.ToggleTaskTip()).Padding(4).Radius(4)
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
				Bind(&a.editHandle).AutoFocus().Label(locales.EditTask()).
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
						b := ui.ButtonBase(c).Label(locales.FilterTag(tag))
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
		playLabel := locales.StartFocus()
		if running {
			playLabel = locales.TimeThisTask()
		}
		ui.Row(c).Width(38).Gap(4).AlignItems(ui.Center).Children(func() {
			if row.Hovered() || bound || selected {
				if bound {
					glyph(c, "timer", icons.timer, 12, p.Ink).Label(locales.TimingThisTask())
				} else if iconButton(c, "play.fill", icons.play, playLabel, p).Disabled(item.Done).Clicked() {
					a.pomodoro.StartTask(item, tier, 0)
				}
				if iconButton(c, "trash", icons.trash, locales.DeleteTask(), p).Clicked() {
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
		if m.Item(locales.MenuEdit()).Chosen() {
			a.beginEdit(c, item, tier)
		}
		m.Submenu(locales.StartFocus(), func(sub *ui.Menu) {
			for _, minutes := range focusPresets {
				if sub.Item(locales.Minutes(minutes)).Disabled(item.Done).Chosen() {
					a.pomodoro.StartTask(item, tier, minutes)
				}
			}
		})
		m.Submenu(locales.MenuQuadrant(), func(sub *ui.Menu) {
			labels := []string{locales.Quadrant1(), locales.Quadrant2(), locales.Quadrant3(), locales.Quadrant4()}
			for i, label := range labels {
				quadrant := todo.Quadrant(i + 1)
				if sub.Item(label).Radio(item.Quadrant == quadrant).Chosen() {
					a.perform(a.store.SetQuadrant(item.ID, quadrant, tier))
				}
			}
			if sub.Item(locales.ClearQuadrant()).Chosen() {
				a.perform(a.store.SetQuadrant(item.ID, todo.QuadrantNone, tier))
			}
		})
		m.Submenu(locales.MenuMoveTo(), func(sub *ui.Menu) {
			for _, target := range []todo.Tier{todo.Today, todo.ThreeDay, todo.Week} {
				if target != tier && sub.Item(tierName(target)).Chosen() {
					a.perform(a.store.Move(item.ID, tier, target))
				}
			}
		})
		m.Separator()
		if m.Item(locales.MenuDelete()).Danger().Chosen() {
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
		placeholder := locales.QuickAddPlaceholder()
		if a.activeTag != "" {
			placeholder = locales.QuickAddTagPlaceholder(a.activeTag)
		}
		input := ui.TextInput(c.Key("quick-add"), &a.quickAdd).Grow(1).Bind(&a.quickAddHandle).Label(locales.QuickAdd()).Placeholder(placeholder).Background(paperColor(c, p)).Border(0, p.Paper).Radius(0).FontSize(13).Padding(6, 0)
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
		if iconButton(c, "arrow.right", icons.arrow, locales.AddToToday(), p).Disabled(strings.TrimSpace(a.quickAdd) == "").Clicked() {
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
		open := textButton(c, "doc.text", icons.file, "todos.md", locales.OpenFile(), p)
		if open.Clicked() {
			a.openTodoFile()
		}
		open.ContextMenu(func(m *ui.Menu) {
			if m.Item(locales.ShowInFolder()).Chosen() {
				mygo.Shell.ShowItemInFolder(a.store.Path())
			}
			if m.Item(locales.CopyPath()).Chosen() {
				mygo.Clipboard.WriteText(a.store.Path())
			}
		})
		ui.Spacer(c)
		if iconButton(c, "slider.horizontal.3", icons.sliders, locales.Settings(), p).Clicked() {
			a.showSettings = !a.showSettings
		}
		if iconButton(c, "questionmark.circle", icons.help, locales.Help(), p).Clicked() {
			a.showHelp = !a.showHelp
			a.showSettings = false
		}
		if textButton(c, "power", icons.quit, locales.Quit(), locales.QuitHibi(), p).Clicked() {
			mygo.App.Quit()
		}
	})
}

func (a *app) helpView(c *ui.Context, p palette) {
	ui.Column(c).Padding(12).Gap(9).Background(paperColor(c, p)).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			glyph(c, "questionmark.circle", icons.help, 13, p.Muted)
			ui.Text(c, locales.HelpTitle()).FontSize(11.5).FontWeight(600).TextColor(p.Ink)
		})
		for _, line := range []string{
			locales.Help1(), locales.Help2(), locales.Help3(), locales.Help4(), locales.Help5(),
			locales.Help6(), locales.Help7(), locales.Help8(), locales.Help9(), locales.Help10(),
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
			ui.Text(c, locales.Settings()).FontSize(13).FontWeight(600).TextColor(p.Ink)
			ui.Spacer(c)
			if iconButton(c, "xmark", icons.close, locales.CloseSettings(), p).Clicked() {
				a.showSettings = false
			}
		})
		ui.Text(c, locales.AutoSenseHeading()).FontSize(10.5).FontWeight(600).TextColor(p.Muted)
		changed := false
		settingRow(c, p, locales.AutoStartTitle(), locales.AutoStartNote(breakMinutes),
			func() {
				changed = ui.Switch(c, &a.autoSense.AutoStart).Label(locales.AutoStartTitle()).Changed() || changed
			})
		settingRow(c, p, locales.EndOnAwayTitle(), locales.EndOnAwayNote(),
			func() {
				changed = ui.Switch(c, &a.autoSense.EndOnAway).Label(locales.EndOnAwayTitle()).Changed() || changed
			})
		watchingAway := a.autoSense.watchesAway()
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, locales.AwayLabel()).FontSize(12).TextColor(p.Ink)
			ui.Row(c).Children(func() { // the track only as wide as its segments
				if ui.Segmented(c, &a.awayIndex, minuteLabels(awayPresets)...).Label(locales.AwayLabel()).Disabled(!watchingAway).Changed() {
					a.autoSense.AwayMinutes = awayPresets[a.awayIndex]
					changed = true
				}
			})
			note := locales.AwayNote()
			if a.autoSense.ActivityAware {
				note = locales.AwayNoteActivity()
			}
			if !watchingAway {
				note = locales.AwayNoteUnused()
			}
			ui.Text(c, note).FontSize(11).TextColor(p.Muted)
		})
		rule(c, p, 1)
		settingRow(c, p, locales.SitSettingTitle(), locales.SitSettingNote(),
			func() {
				changed = ui.Switch(c, &a.autoSense.SitReminder).Label(locales.SitSettingTitle()).Changed() || changed
			})
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, locales.SitLabel()).FontSize(12).TextColor(p.Ink)
			ui.Row(c).Children(func() { // the track only as wide as its segments
				if ui.Segmented(c, &a.sitIndex, minuteLabels(sitPresets)...).Label(locales.SitLabel()).Disabled(!a.autoSense.SitReminder).Changed() {
					a.autoSense.SitMinutes = sitPresets[a.sitIndex]
					changed = true
				}
			})
			ui.Text(c, locales.SitStudy()).FontSize(11).TextColor(p.Muted)
		})
		rule(c, p, 1)
		settingRow(c, p, locales.ActivityTitle(), locales.ActivityNote(),
			func() {
				changed = ui.Switch(c, &a.autoSense.ActivityAware).Label(locales.ActivityTitle()).Changed() || changed
			})
		if !a.autoSense.watches() && !a.autoSense.ActivityAware {
			ui.Text(c, locales.AllOffNote()).FontSize(11).TextColor(p.Muted)
		}
		if changed {
			a.setAutoSense(a.autoSense)
		}
		rule(c, p, 1)
		ui.Text(c, locales.LanguageHeading()).FontSize(10.5).FontWeight(600).TextColor(p.Muted)
		langs := mygo.App.Languages()
		names := make([]string, len(langs))
		for i, tag := range langs {
			names[i] = i18n.Autonym(tag)
		}
		shown := slices.Index(langs, c.Locale())
		ui.Row(c).Children(func() {
			if ui.Segmented(c, &shown, names...).Label(locales.LanguageHeading()).Changed() {
				mygo.App.SetLocale(langs[shown]) // kept for the next launches
			}
		})
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
			a.setStatus(locales.StatusEmptyTextKept())
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
	streak := timer.StreakSeconds / 60
	ui.Column(c).Fill().Background(p.Paper).Children(func() {
		ui.Column(c).Padding(16).Center().Gap(9).Background(p.Paper).Children(func() {
			glyph(c, "", icons.cat, 26, p.Muted)
			ui.Text(c, locales.PromptTitle()).FontSize(16).FontWeight(600).TextColor(p.Ink)
		})
		rule(c, p, 1)
		ui.Column(c).Fill().Center().Padding(16, 20).Gap(12).Children(func() {
			ui.Column(c).Center().Gap(5).Children(func() {
				ui.Text(c, locales.PromptDone(timer.SessionFocusMinutes)).Font("Menlo").FontSize(14).TextColor(p.Ink)
				// A break skipped since the last rest: say how long it has been.
				if streak > timer.SessionFocusMinutes {
					ui.Text(c, locales.PromptStreak(streak)).FontSize(12).FontWeight(600).TextColor(p.Ink).TextAlign(ui.Center)
				}
			})
			ui.Column(c).Center().Gap(3).Children(func() {
				ui.Text(c, breakTip(max(streak, timer.SessionFocusMinutes))).FontSize(12).TextColor(p.Ink).TextAlign(ui.Center)
				ui.Text(c, locales.PromptRest(breakMinutes)).FontSize(11).TextColor(p.Muted)
			})
			ui.Row(c).Gap(12).Children(func() {
				if textButton(c, "arrow.right", icons.arrow, locales.Skip(), locales.SkipBreak(), p).Clicked() {
					a.pomodoro.Skip()
				}
				if textButton(c, "", icons.catAsleep, locales.StartBreak(), locales.StartBreak(), p).Clicked() {
					a.pomodoro.StartBreak()
				}
			})
		})
	})
}

// breakTip suggests what to do in the break after minutes of focus in a
// row: the longer the user sat, the more the body needs.
func breakTip(minutes int) string {
	switch {
	case minutes < 45:
		return locales.TipShort()
	case minutes < 90:
		return locales.TipMedium()
	case minutes < 150:
		return locales.TipLong()
	}
	return locales.TipVeryLong()
}

// tierName is a tier's name as the panel shows it.
func tierName(t todo.Tier) string {
	switch t {
	case todo.ThreeDay:
		return locales.TierThreeDay()
	case todo.Week:
		return locales.TierWeek()
	}
	return locales.TierToday()
}

// headerDate is today's date in the header, as "Fri, Nov 27" or
// "11月27日周五", in locale.
func headerDate(locale, date string) string {
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return date
	}
	return i18n.FormatDate(locale, day, i18n.DateOptions{Skeleton: "MMMEd"})
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
