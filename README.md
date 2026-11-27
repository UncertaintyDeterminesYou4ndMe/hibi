# Hibi

A tiny, fast macOS menu bar todo app backed by a single plain Markdown file. No database, no sync service, no accounts — just `~/todos.md` and a menu bar icon.

The app is written in **Go** with [MyGo](https://github.com/egoist/mygo) native UI: no webview, JavaScript, cgo, or frontend toolchain.

**Hibi** (日々) is Japanese for "day after day" — the everyday rhythm of showing up and getting things done.

## Features

- Menu bar todo list with three time horizons: **Today**, **3-Day**, and **Week**
- Eisenhower quadrant markers (`!1`–`!4`) for prioritizing tasks by importance/urgency
- Task-bound Pomodoro timer with a selectable focus length (5/15/30/40/50/60 min, remembers your last pick) and a 10 min break, with automatic per-task time tracking
- Focus-end reminder: a centered, always-on-top prompt that stays until you answer it; the break countdown only starts when you click Start Break, and the chime repeats every minute (up to 10 times) while it waits. Break-end reminder: a system notification + chime
- The focus-end prompt suggests what to do in the break, by how long you have focused without a rest: under 45 minutes, use the restroom, get water and walk around; under 90, also loosen your neck, shoulders and wrists; under 150, climb stairs or do squats; longer, climb stairs and do push-ups. When you skipped a break since you last rested, it also says how long you have focused in a row. A rest is a break that ran to its end, a break skipped after 3 minutes, or leaving the computer
- Automatic timing that follows you at the computer, so you never have to remember to start it. Each part can be turned off, and the away time changed, in the panel's settings (the sliders button in the toolbar):
  - When you are at the computer and nothing is being timed, a focus session starts by itself (your last-picked length, no task). This happens at launch, when you come back after leaving, and when a break ends.
  - Leaving the computer — 5 minutes (3, 10, 15 or 30 in the settings) without keyboard or mouse input, or the Mac asleep that long — ends the session and counts as your break. The task is credited for the time before you left, and a waiting break prompt closes.
  - Skipping a break goes straight into the next focus session.
  - **Stop** ends the session, but time at the computer still counts as focus. If you are still there 10 minutes (the break length) later, a session starts that began at the stop. If you leave, the time before you left is recorded, and the next session starts when you are back. To not be timed, pause instead.
  - A sitting reminder: after 30 minutes at the computer (45 or 60 in the settings) without a break of 3 minutes, a notification asks you to get up and walk for 3 minutes. The focus countdown keeps running, and a walk shorter than the away time does not end the session. Three minutes without input counts as having got up and starts the sitting time over. In a small trial (Duran et al. 2023), a 5-minute walk every 30 minutes lowered both blood sugar and blood pressure, and walks every 60 minutes did not lower blood sugar. So a long focus session (50 or 60 minutes) can still break up the sitting.
  - Activity awareness (macOS): while a video plays or a call goes on, not touching the computer counts as being at it, not as leaving or getting up. When focus ends while you type, the prompt and its chime wait for a pause of 8 seconds (3 minutes at most); in a call, for the call to end (an hour at most). A sitting reminder also waits for the call to end. Hibi reads only system state: whether a process other than a keep-awake tool keeps the display awake, whether the default microphone is in use, and the time since the last key press. It takes no screenshots and reads no window, screen or audio content.
  - With all of these off, Hibi does not read the keyboard and mouse idle time at all.
  - Known limit: reading or watching a video for the away time without touching anything also counts as leaving.
- The menu bar icon is a cat's head that tires as the day goes on. Each sitting reminder (30/45/60 minutes without a break) makes it one step more tired, and getting up rests it; 4, 6 and 8 hours of focus in a day make it tired, weary and exhausted until the next day. The more tiring of the two decides, and hovering over the icon says why. During a break it sleeps. The sitting counts only while the sitting reminder is on. The menu bar shows no time left: during a focus it shows the icon alone, while paused or on a break only `暂停` or `休息`, and `该休息了` when a focus has ended; the panel counts the seconds. With no timer it shows today's open tasks
- Today's focus total in the panel header (`专注 1h25m`), sessions without a task included
- A global shortcut (default `⌃⌥H`) opens the panel from any app with the quick-add field focused; closing the panel gives the keyboard back to the app you were in
- The task list works from the keyboard (see below)
- Tags (`#tag`) with click-to-filter
- Edit any task in place — double-click a row and rewrite it with the same `text #tag !n` grammar the quick-add field accepts
- English and Simplified Chinese: Hibi follows the system's languages, and the settings switch it (kept for the next launches). The messages are in `locales/` (`en.json` is the source, `zh-Hans.json` the translation); `go tool mygo generate` writes `locales/catalog_gen.go` from them
- Everything stored in a plain Markdown file (`~/todos.md`) — open it in any editor, sync it with any tool, edit it by hand
- Automatic date rollover: completed tasks are archived, unfinished tasks carry forward
- MyGo native UI, with a standard-library-only task/Markdown core and platform integration for tray, windows, notifications, file watching and shell actions

## Build from source

Requires **Go 1.27.3+**. MyGo is pinned to `v0.8.3` in `go.mod`; Go downloads the module dependencies automatically. On macOS, MyGo assembles and ad-hoc signs the app bundle — no Xcode project or frontend package manager.

```bash
bash build.sh
```

This runs the tests and produces:
- `Hibi` — standalone executable
- `build/darwin-<arch>/Hibi.app` — an ad-hoc signed app bundle, MyGo’s native build artifact

On macOS it then moves that `Hibi.app` into `/Applications`, where Spotlight and Launchpad find it, quits a running Hibi and opens the new one. `bash scripts/install-launchagent.sh` opens that Hibi at login.

For development with automatic rebuilds, run `go tool mygo dev`.

### Gatekeeper

`Hibi.app` is signed ad-hoc (`codesign --sign -`), not with a paid Apple Developer certificate. The first time you open it, macOS Gatekeeper will refuse a normal double-click. To open it anyway:

- Right-click `Hibi.app` → **Open** → confirm in the dialog that appears, **or**
- **System Settings → Privacy & Security**, scroll down, and click **Open Anyway** next to the Hibi warning.

You only need to do this once.

## Data format

All data lives in `~/todos.md`. It's plain Markdown, editable with anything, and the app watches the file for external changes and reloads automatically.

```markdown
# Todos

## 今日 2026-07-17

- [ ] Task A #project-x !1
- [x] Task B ⏱40m

## 三日

- [ ] Task C #project-x #urgent !2

## 本周

- [ ] Task D !3 ⏱125m

## 归档

### 2026-07-16

- [x] Finished yesterday
```

- `## 今日 YYYY-MM-DD` — Today's tasks; the only tier with a date.
- `## 三日` — 3-Day tasks (no date/id suffix).
- `## 本周` — Week tasks (no date/id suffix). Legacy files with an ISO week id (`## 本周 YYYY-Www`) still parse without data loss and get rewritten to the bare form on next save.
- `## 归档` — Archive; content is kept as-is, newest block prepended to the top.

A task line is always `- [ ] text` or `- [x] text`. The text may carry, in this **canonical order** once written back to disk:

```
text #tag1 #tag2 !n ⏱Nm
```

- `#tag` — zero or more tags, anywhere in the raw text; extracted, de-duplicated by first occurrence, and re-emitted right after the text.
- `!n` (`!1`–`!4`) — Eisenhower quadrant marker, trailing only:

  | Marker | Meaning |
  | --- | --- |
  | `!1` | Important & urgent |
  | `!2` | Important, not urgent |
  | `!3` | Urgent, not important |
  | `!4` | Neither |

- `⏱Nm` — cumulative focus minutes tracked by the Pomodoro timer, trailing only, e.g. `⏱85m`.

Both suffix markers only count at the very end of the line (in either relative order) and are re-serialized in the fixed order above; tags always come before them.

## Keyboard shortcuts / interaction

| Shortcut | Action |
| --- | --- |
| `↩` (Return) | Add to Today |
| `⌥↩` (Option+Return) | Add to 3-Day |
| `⌘↩` (Command+Return) | Add to Week |
| Double-click a row | Edit that task in place |
| `↩` while editing | Save |
| `esc` while editing | Discard the edit |
| `⌃⌥H` (anywhere) | Open or close the panel |
| `↑` / `↓` in quick add | Select the last / first task |
| `↑` / `↓` on a task | Move the selection (`↓` past the last task goes back to quick add) |
| `space` on a task | Complete / reopen it |
| `↩` on a task | Edit it in place |
| `F` on a task | Time it (as its play button does) |
| `1`–`4`, `0` on a task | Set its quadrant, or clear it |
| `⌘↑` / `⌘↓` on a task | Move it to the section above / below (Today, 3-Day, Week); it stays selected |
| `⌘⌫` on a task | Delete it |
| `esc` | From the list: back to quick add. From quick add: close the panel |

- **In-place editing**: double-click a task to turn the row into a text field prefilled with its raw form (`text #tag !n`) — the same grammar the quick-add field takes. `↩` saves, `esc` discards, clicking elsewhere saves. The accumulated `⏱Nm` is not shown (the timer owns it) and survives an edit; typing an explicit `⏱Nm` overrides it. Input that parses to an empty task is refused rather than blanking the row.
- Click a tag to filter all three sections down to it; click again (or the close button in the filter bar) to clear.
- Hover a task row: the play button starts a bound focus session for that task (using your last-picked length), and the trash icon deletes it. While a session already runs, the play button moves the session to that task instead and the countdown continues: the previous task keeps the minutes it was timed, and the new task is credited from now on.
- Right-click a task: edit, start focus (an explicit length always starts a new session), set/clear quadrant, move it to another tier, or delete.
- Bottom toolbar: **todos.md** (opens `~/todos.md` in its default app; right-click to reveal it or copy the path), the help icon (inline usage help), and **退出**.

## Energy

Hibi runs all day, so its background cost is its performance. While a session runs with the panel closed, the timer wakes only when the menu bar's minutes change or the session ends (a deadline on the monotonic clock, which stands still while the Mac sleeps, not a one-second ticker), and the idle time is read only when you could first count as away: at most once a minute while you are at the computer, every 5 s while you are away, and at once when the Mac wakes, the screen unlocks or a break ends. With the panel open it still ticks every second.

Measured on one Mac over the same 60 s, both with a session running and the panel closed: the previous build (which showed `mm:ss` in the menu bar and updated it every second) used 0.18 s of CPU time and woke 89 times; this build uses 0.01 s and wakes 10 times.

Opening the panel used to cost memory that never came back, about 4.5 MB each time: on one Mac the previous build grew from 16 MB at launch to 53 MB after five opens, and kept growing. On macOS 26 every NSPopover that has shown leaves its material's backdrop behind after it is released (a 4.5 MB graphics surface), and Hibi made a new popover for each open. Hibi now makes the panel once and hides it on close, so it has one popover for its whole run. Measured over ten opens (by the icon, by the shortcut and Escape, and closed by switching apps): 39–44 MB throughout, with no growth from one open to the next. After a close, Hibi also hands free memory back to the system (the Go heap and malloc).

## Settings

`~/Library/Application Support/Hibi/settings.json` holds the remembered focus length, the automatic timing settings and, optionally, the global shortcut:

```json
{
  "focusMinutes": 40,
  "hotkey": "Ctrl+Alt+H",
  "autoSense": {
    "autoStart": true,
    "endOnAway": true,
    "awayMinutes": 5,
    "sitReminder": true,
    "sitMinutes": 30,
    "activityAware": true
  }
}
```

The panel's settings write `autoSense` and apply it at once. `awayMinutes` takes 3, 5, 10, 15 or 30, and `sitMinutes` 30, 45 or 60; a field left out keeps its default (on, on, 5, on, 30, on).

`hotkey` takes names such as `Ctrl`, `Alt`, `Shift`, `Cmd` joined with `+`; `"off"` turns the shortcut off. The default avoids `⌃Space` and `⌃⌥Space` (input source switching) and `⌥Space` (ChatGPT, Claude). Restart Hibi after editing the file.

`focus.json` in the same folder records the focus seconds of each day for the header total.

## Look

Monochrome and high-contrast, following the system light/dark setting. No hue anywhere: task priority reads as ink density on the rail at the left edge of each row (`!1` solid, then three lighter grays down to `!4`), tags are plain `#name` text, and the active tag filter inverts. Text uses two grays only, ink and a secondary gray, and both meet WCAG 2.2 AA (4.5:1) on every background a row can have, the popover material included; the rails, check circles and the plus sign meet 3:1. `design_test.go` checks this. The row under the pointer and the row selected from the keyboard have different fills. Rules are 1px and radii are 4px. Controls draw Apple's SF Symbols (with a same-grid custom monochrome 24×24 SVG set as fallback), and quick add plus the bottom toolbar stay visible while tasks and help scroll.

The panel shows as a native system popover: AppKit draws its material, rounded corners, shadow and the arrow to the tray icon, and the view stays transparent where the material shows. Each frame reads the todo store and the timer once and passes those snapshots down, and the popover window is kept above ordinary windows without taking the input method's popup level.

## Self-test

```bash
./Hibi --selftest
```

Runs built-in smoke assertions with no GUI and exits 0 after printing `ALL PASS` on success. For the complete suite, run:

```bash
go test ./...
```

Tests use temp directories, never your real `~/todos.md`. They cover parsing/serialization round-trips, legacy migration, tags and markers, task operations, Pomodoro transitions/settlement, rollover, file I/O, and headless MyGo UI interactions (quick-add shortcuts, context menus, editing, filtering and moving tasks, the keyboard-driven list) and the palette's WCAG contrast.

## Source layout

- `main.go`, `app.go` — process lifecycle, tray/flyout, file watching and desktop integration
- `panel.go` — MyGo native panel, task rows, context menus and break prompt
- `design.go` — light/dark palettes and shared monochrome components (`design_test.go` checks their contrast)
- `memory_darwin.go` — hands malloc's free memory back to the system after the panel closes
- `icons.go` — custom 24×24 SVG icon set
- `icon.go`, `mood.go` — the menu bar icons: a cat's head in five moods drawn by `scripts/gen-tray-icon.swift` into `icon/tray*.png`, and which mood the day's focus and the sitting call for
- `scripts/gen-tray-icon.swift` — draws those PNGs with CoreGraphics when the shape or its size changes
- `pomodoro.go` — concurrency-safe focus/break state machine
- `presence.go` — tells the timer when you leave and when you are back, from the keyboard/mouse idle time (`idle_darwin.go` reads it from Core Graphics off the main thread)
- `focuslog.go` — per-day focus seconds behind the header total
- `internal/todo/` — UI-independent Markdown model, parser, atomic persistence, rollover and task store
- `*_test.go` — core, timer and headless native-UI tests
- `mygo.json` — app identity, icon, version and packaging configuration

---

## 中文说明

Hibi（日々，"日复一日"）现在使用 Go + MyGo 原生 UI 实现，不使用 WebView、JavaScript 或 cgo。它仍是零账号、零数据库、纯 Markdown 的 macOS 菜单栏待办应用，数据完全保存在 `~/todos.md`，可以用任意编辑器直接打开修改。核心能力包括：今日/三日/本周三档时间层级、四象限优先级标记、双击原地编辑（`正文 #标签 !象限`，回车保存，Esc 放弃）、绑定任务的番茄钟（5/15/30/40/50/60 分钟并记住选择，休息 10 分钟，累计专注时长自动写回；专注结束显示置顶提醒，未应答时每分钟提示，最多 10 次；休息结束发系统通知和提示音）、自动计时（你在电脑前且没有在计时时自动开始一轮专注：启动时、离开后回来时、休息结束后；5 分钟无键鼠输入或睡眠视为离开，本轮结束并算作休息，任务只记离开前的时长；久坐提醒（连续坐 30/45/60 分钟、中间没有 3 分钟以上的停顿，就发通知提醒起来走 3 分钟，倒计时不停）；识别视频、会议和打字（播放视频或开会时不碰电脑也不算离开；专注结束时正在打字就等停顿 8 秒再提醒，最多 3 分钟，开会就等会议结束，最多 1 小时；只读系统状态，不截屏、不读内容）；自动开始、离开结束、离开判定时长（3/5/10/15/30 分钟）、久坐提醒和识别视频会议打字都可以在面板的设置里调整，全部关闭时不读取键鼠空闲时间；跳过休息直接进入下一轮；手动停止后在电脑前的时间仍算专注（10 分钟后还没离开，新一轮从停止时算起；离开就记到离开前，回来再开始；不想计时用暂停）；计时中点任务的播放按钮会把当前这一轮改记到该任务，倒计时不重置）、菜单栏图标是一只小猫的头，会随一天的工作慢慢变累（每到一次久坐提醒就累一档，起身后恢复；当天专注满 4、6、8 小时分别是有点累、很累、累坏了，到第二天才恢复；取两者中更累的一档，鼠标悬停能看到原因；久坐这一项只在开着久坐提醒时计算），休息时闭眼睡觉，不显示剩余时间（专注中只有图标，暂停、休息时只显示“暂停”“休息”）、面板顶部显示今日专注总时长（含未绑定任务的轮次）、全局快捷键 ⌃⌥H 在任何应用里呼出面板（可在 settings.json 的 hotkey 修改，关闭面板后焦点回到原应用）、任务列表键盘操作（输入框里按 ↑↓ 进入列表；空格完成、↩ 编辑、F 计时、1~4 设象限、0 清除、⌘⌫ 删除、⌘↑↓ 移到上一档或下一档、Esc 回到输入框）、标签筛选、外部文件变更自动重载，以及跨日完成项归档。

构建：安装 Go 1.27.3+ 后运行 `bash build.sh`，会执行测试并产出 `Hibi` 和 `build/` 下的 `Hibi.app`，在 macOS 上会把 `Hibi.app` 移进 `/Applications`（Spotlight 和启动台能找到），并打开它；`bash scripts/install-launchagent.sh` 设为登录时打开。开发时可运行 `go tool mygo dev`。自测：`./Hibi --selftest`；完整测试：`go test ./...`。首次打开 ad-hoc 签名应用时，按上文 Gatekeeper 一节右键打开。数据文件格式与快捷键保持兼容，详见上文。
