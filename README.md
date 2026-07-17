# Hibi

A tiny, fast macOS menu bar todo app backed by a single plain Markdown file. No database, no sync service, no accounts — just `~/todos.md` and a menu bar icon.

**Hibi** (日々) is Japanese for "day after day" — the everyday rhythm of showing up and getting things done.

## Features

- Menu bar todo list with three time horizons: **Today**, **3-Day**, and **Week**
- Eisenhower quadrant markers (`!1`–`!4`) for prioritizing tasks by importance/urgency
- Task-bound Pomodoro timer (40 min focus / 10 min break) with automatic per-task time tracking
- Tags (`#tag`) with click-to-filter, deterministic per-tag colors
- Everything stored in a plain Markdown file (`~/todos.md`) — open it in any editor, sync it with any tool, edit it by hand
- Automatic date rollover: completed tasks are archived, unfinished tasks carry forward
- Zero third-party dependencies — pure SwiftUI + Foundation + AppKit + UserNotifications, single file

## Build from source

Requires Xcode Command Line Tools (`swiftc`, `codesign`) — no Xcode project, no package manager.

```bash
bash build.sh
```

This produces:
- `Hibi` — standalone executable
- `Hibi.app` — an ad-hoc signed app bundle

Drag `Hibi.app` into `/Applications` (or run it in place).

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

- Click a tag chip to filter all three sections down to that tag; click again (or the `✕` in the filter bar) to clear.
- Hover a task row: `▶` starts a bound 40-minute focus session for that task, `×` deletes it.
- Right-click a task: start focus, set/clear quadrant, move it to another tier, or delete.
- Bottom toolbar: **Open File** (opens `~/todos.md` in your default editor), `?` (quick usage popover), **Quit**.

UI inspired by Minto.

## Self-test

```bash
./Hibi --selftest
```

Runs a suite of built-in assertions with no GUI, using only temp directories — never touches your real `~/todos.md`. Covers parsing/serialization round-trips, legacy week-id migration, quadrant and duration marker parsing, tag extraction, Pomodoro settlement, rollover, sorting, and file I/O edge cases. Prints `ALL PASS` and exits 0 on success, or prints each `FAIL` and exits 1 otherwise.

---

## 中文说明

Hibi（日々，"日复一日"）是一个纯 SwiftUI 实现的 macOS 菜单栏待办应用，零第三方依赖、单文件源码，数据完全保存在 `~/todos.md` 这个纯 Markdown 文件里，可以用任意编辑器直接打开修改。核心能力包括：今日/三日/本周三档时间层级、四象限优先级标记、绑定任务的番茄钟（专注 40 分钟 + 休息 10 分钟，自动记录每个任务的累计专注时长）、标签筛选。构建方式见上文 **Build from source**（`bash build.sh` 产出 `Hibi.app`），首次打开因为是 ad-hoc 签名需要按上文 **Gatekeeper** 一节右键打开。数据文件格式、四象限/标签/专注时长标记的具体语法见上文 **Data format**，快捷键见 **Keyboard shortcuts / interaction**，自测方式见 **Self-test**。
