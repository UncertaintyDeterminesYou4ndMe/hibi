import SwiftUI
import AppKit
import Foundation
import UserNotifications

// MARK: - Models

enum Quadrant: Int, CaseIterable, Equatable {
    case q1 = 1 // 重要且紧急
    case q2 = 2 // 重要不紧急
    case q3 = 3 // 紧急不重要
    case q4 = 4 // 不重要不紧急
}

enum Tier {
    case today, threeDay, week
}

struct TodoItem: Identifiable, Equatable {
    let id = UUID()
    var text: String
    var done: Bool
    var quadrant: Quadrant? = nil
    var focusMinutes: Int = 0
    var tags: [String] = []

    /// Everything except `id`. `id` is minted fresh on every parse, so `==` is useless
    /// for answering "did the file actually change?" — see `TodoData.sameContent(as:)`.
    func sameContent(as other: TodoItem) -> Bool {
        text == other.text && done == other.done && quadrant == other.quadrant
            && focusMinutes == other.focusMinutes && tags == other.tags
    }

    /// The raw text an in-place editor starts from — what the user would have typed
    /// into the quick-add field to produce this item. `⏱Nm` is left out: the timer
    /// owns it.
    var editableText: String {
        ([text] + tags.map { "#\($0)" }
            + (quadrant.map { ["!\($0.rawValue)"] } ?? [])).joined(separator: " ")
    }
}

struct TodoData: Equatable {
    var todayDate: String
    var todayItems: [TodoItem]
    var threeDayItems: [TodoItem]
    var weekItems: [TodoItem]
    var archiveBody: String

    static func empty(todayDate: String) -> TodoData {
        TodoData(todayDate: todayDate, todayItems: [], threeDayItems: [], weekItems: [], archiveBody: "")
    }

    /// Equality ignoring every item's `id`. Used to tell a genuine external edit apart
    /// from the file-watcher echo of our own `persist()`: on an echo we keep the
    /// in-memory `data` (and therefore every item's `id`), so an in-place edit or a
    /// hover highlight survives saving.
    func sameContent(as other: TodoData) -> Bool {
        guard todayDate == other.todayDate, archiveBody == other.archiveBody else { return false }
        func same(_ a: [TodoItem], _ b: [TodoItem]) -> Bool {
            a.count == b.count && zip(a, b).allSatisfy { $0.sameContent(as: $1) }
        }
        return same(todayItems, other.todayItems)
            && same(threeDayItems, other.threeDayItems)
            && same(weekItems, other.weekItems)
    }
}

// MARK: - Date helpers

enum DateUtil {
    static func todayDateString(for date: Date = Date()) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone.current
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter.string(from: date)
    }

    /// "7月17日 周五" style header for a "yyyy-MM-dd" date string.
    static func headerDisplay(for dateString: String) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone.current
        formatter.dateFormat = "yyyy-MM-dd"
        guard let date = formatter.date(from: dateString) else { return dateString }
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = TimeZone.current
        let comps = cal.dateComponents([.month, .day, .weekday], from: date)
        let weekdaySymbols = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"]
        let weekdayIdx = (comps.weekday ?? 1) - 1
        let weekdayStr = weekdaySymbols[max(0, min(6, weekdayIdx))]
        return "\(comps.month ?? 0)月\(comps.day ?? 0)日 \(weekdayStr)"
    }
}

// MARK: - Parsing / Serialization

enum TodoParser {

    static func parse(_ text: String) -> TodoData {
        var todayDate = ""
        var todayItems: [TodoItem] = []
        var threeDayItems: [TodoItem] = []
        var weekItems: [TodoItem] = []
        var archiveLines: [String] = []

        enum Section { case none, today, threeDay, week, archive }
        var current: Section = .none

        let lines = text.components(separatedBy: "\n")
        var idx = 0
        while idx < lines.count {
            let line = lines[idx]
            if let m = match(line, pattern: #"^##\s*今日\s+(.+?)\s*$"#) {
                todayDate = m
                current = .today
            } else if match(line, pattern: #"^##\s*三日\s*$"#) != nil {
                current = .threeDay
            } else if match(line, pattern: #"^##\s*本周(?:\s|$)"#) != nil {
                // Compatible with legacy "## 本周 YYYY-Www" — the week id (if any) is discarded;
                // on next serialize it is rewritten as bare "## 本周".
                current = .week
            } else if match(line, pattern: #"^##\s*归档\s*$"#) != nil {
                current = .archive
                // Everything remaining (after this line) belongs to archive, raw.
                idx += 1
                while idx < lines.count {
                    archiveLines.append(lines[idx])
                    idx += 1
                }
                break
            } else {
                switch current {
                case .today:
                    if let item = parseItemLine(line) { todayItems.append(item) }
                case .threeDay:
                    if let item = parseItemLine(line) { threeDayItems.append(item) }
                case .week:
                    if let item = parseItemLine(line) { weekItems.append(item) }
                case .none, .archive:
                    break
                }
            }
            idx += 1
        }

        let archiveBody = archiveLines.joined(separator: "\n")
            .trimmingCharacters(in: .whitespacesAndNewlines)

        return TodoData(todayDate: todayDate, todayItems: todayItems, threeDayItems: threeDayItems, weekItems: weekItems, archiveBody: archiveBody)
    }

    private static func parseItemLine(_ line: String) -> TodoItem? {
        var text: String
        var done: Bool
        if let t = match(line, pattern: #"^-\s*\[\s\]\s*(.*)$"#) {
            text = t
            done = false
        } else if let t = match(line, pattern: #"^-\s*\[[xX]\]\s*(.*)$"#) {
            text = t
            done = true
        } else {
            return nil
        }
        let (afterSuffix, quadrant, minutes) = stripSuffixMarkers(text)
        let (clean, tags) = extractTags(afterSuffix)
        return TodoItem(text: clean, done: done, quadrant: quadrant, focusMinutes: minutes ?? 0, tags: tags)
    }

    /// Strips a trailing "!1".."!4" quadrant marker (with or without a preceding space)
    /// from the end of a line's text. A marker only counts at the very end of the
    /// string — occurrences in the middle of the text are left untouched.
    static func stripQuadrantSuffix(_ text: String) -> (String, Quadrant?) {
        guard let regex = try? NSRegularExpression(pattern: #"^(.*?)\s*!([1-4])$"#) else { return (text, nil) }
        let range = NSRange(text.startIndex..., in: text)
        guard let m = regex.firstMatch(in: text, range: range),
              let baseRange = Range(m.range(at: 1), in: text),
              let qRange = Range(m.range(at: 2), in: text),
              let qVal = Int(text[qRange]),
              let quadrant = Quadrant(rawValue: qVal) else {
            return (text, nil)
        }
        return (String(text[baseRange]), quadrant)
    }

    /// Strips a trailing "⏱<minutes>m" focus-duration marker (with or without a
    /// preceding space) from the end of a line's text. Only matches at the very end.
    static func stripDurationSuffix(_ text: String) -> (String, Int?) {
        guard let regex = try? NSRegularExpression(pattern: #"^(.*?)\s*⏱(\d+)m$"#) else { return (text, nil) }
        let range = NSRange(text.startIndex..., in: text)
        guard let m = regex.firstMatch(in: text, range: range),
              let baseRange = Range(m.range(at: 1), in: text),
              let numRange = Range(m.range(at: 2), in: text),
              let minutes = Int(text[numRange]) else {
            return (text, nil)
        }
        return (String(text[baseRange]), minutes)
    }

    /// Strips both a trailing quadrant marker and a trailing focus-duration marker from
    /// the end of a line's text, in either order (e.g. "!1 ⏱85m" or "⏱85m !1"). Only
    /// markers at the very end of the string are recognized; either, both, or neither
    /// may be present.
    static func stripSuffixMarkers(_ text: String) -> (String, Quadrant?, Int?) {
        var remaining = text
        var quadrant: Quadrant? = nil
        var minutes: Int? = nil
        var progress = true
        while progress {
            progress = false
            if quadrant == nil {
                let (clean, q) = stripQuadrantSuffix(remaining)
                if let q = q {
                    remaining = clean
                    quadrant = q
                    progress = true
                }
            }
            if minutes == nil {
                let (clean, m) = stripDurationSuffix(remaining)
                if let m = m {
                    remaining = clean
                    minutes = m
                    progress = true
                }
            }
        }
        return (remaining, quadrant, minutes)
    }

    /// Extracts all `#tag` tokens (pattern `#[^\s#]+`) from anywhere in `text`, in
    /// order of first appearance, de-duplicated (a lone `#` with nothing attached does
    /// not count as a tag — the pattern requires at least one following non-space,
    /// non-`#` character). Returns the display text with tag tokens removed and any
    /// resulting run of whitespace collapsed to a single space, trimmed.
    static func extractTags(_ text: String) -> (String, [String]) {
        guard let regex = try? NSRegularExpression(pattern: #"#[^\s#]+"#) else { return (text, []) }
        let range = NSRange(text.startIndex..., in: text)
        let matches = regex.matches(in: text, range: range)
        guard !matches.isEmpty else { return (text, []) }

        var tags: [String] = []
        var seen = Set<String>()
        for m in matches {
            guard let r = Range(m.range, in: text) else { continue }
            let tag = String(text[r].dropFirst())
            if !seen.contains(tag) {
                seen.insert(tag)
                tags.append(tag)
            }
        }

        var cleaned = text
        for m in matches.reversed() {
            guard let r = Range(m.range, in: cleaned) else { continue }
            cleaned.replaceSubrange(r, with: " ")
        }
        cleaned = cleaned.replacingOccurrences(of: #"\s+"#, with: " ", options: .regularExpression)
        cleaned = cleaned.trimmingCharacters(in: .whitespaces)

        return (cleaned, tags)
    }

    private static func match(_ line: String, pattern: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return nil }
        let range = NSRange(line.startIndex..., in: line)
        guard let m = regex.firstMatch(in: line, range: range) else { return nil }
        if m.numberOfRanges > 1, let r = Range(m.range(at: 1), in: line) {
            return String(line[r])
        }
        return ""
    }

    static func serialize(_ data: TodoData) -> String {
        var lines: [String] = []
        lines.append("# Todos")
        lines.append("")
        lines.append("## 今日 \(data.todayDate)")
        lines.append("")
        for item in data.todayItems {
            lines.append(serializeItem(item))
        }
        lines.append("")
        lines.append("## 三日")
        lines.append("")
        for item in data.threeDayItems {
            lines.append(serializeItem(item))
        }
        lines.append("")
        lines.append("## 本周")
        lines.append("")
        for item in data.weekItems {
            lines.append(serializeItem(item))
        }
        lines.append("")
        lines.append("## 归档")
        lines.append("")
        if !data.archiveBody.isEmpty {
            lines.append(data.archiveBody)
        }
        var result = lines.joined(separator: "\n")
        if !result.hasSuffix("\n") { result += "\n" }
        return result
    }

    static func serializeItem(_ item: TodoItem) -> String {
        var textPart = item.text
        if !item.tags.isEmpty {
            textPart += " " + item.tags.map { "#\($0)" }.joined(separator: " ")
        }
        var suffix = ""
        if let q = item.quadrant { suffix += " !\(q.rawValue)" }
        if item.focusMinutes > 0 { suffix += " ⏱\(item.focusMinutes)m" }
        return "- [\(item.done ? "x" : " ")] \(textPart)\(suffix)"
    }
}

// MARK: - Sorting (display only — does not affect stored / serialized order)

enum TodoSort {
    /// Orders items by quadrant priority (1 < 2 < 3 < 4 < unclassified), keeping file
    /// order within the same quadrant; completed items always sort to the end.
    static func sorted(_ items: [TodoItem]) -> [TodoItem] {
        items.enumerated().sorted { a, b in
            let doneA = a.element.done ? 1 : 0
            let doneB = b.element.done ? 1 : 0
            if doneA != doneB { return doneA < doneB }
            let qa = quadrantRank(a.element.quadrant)
            let qb = quadrantRank(b.element.quadrant)
            if qa != qb { return qa < qb }
            return a.offset < b.offset
        }.map { $0.element }
    }

    private static func quadrantRank(_ q: Quadrant?) -> Int {
        switch q {
        case .q1: return 0
        case .q2: return 1
        case .q3: return 2
        case .q4: return 3
        case .none: return 4
        }
    }
}

// MARK: - Rollover

enum Rollover {

    /// Applies date rollover to freshly-parsed data. If "今日" date differs from the
    /// current date, all completed items across the three tiers are moved into a single
    /// archive sub-section titled with the old date; unfinished items stay in place;
    /// "今日" date is updated to today. Returns (data, changed).
    static func apply(_ input: TodoData, now: Date = Date()) -> (TodoData, Bool) {
        var data = input

        let curDate = DateUtil.todayDateString(for: now)
        guard data.todayDate != curDate else { return (data, false) }

        let oldDate = data.todayDate

        let todayDone = data.todayItems.filter { $0.done }
        data.todayItems = data.todayItems.filter { !$0.done }

        let threeDayDone = data.threeDayItems.filter { $0.done }
        data.threeDayItems = data.threeDayItems.filter { !$0.done }

        let weekDone = data.weekItems.filter { $0.done }
        data.weekItems = data.weekItems.filter { !$0.done }

        let archivedItems = todayDone + threeDayDone + weekDone
        if !archivedItems.isEmpty {
            let title = oldDate.isEmpty ? curDate : oldDate
            let block = archiveBlock(title: title, items: archivedItems)
            data.archiveBody = prepend(block, to: data.archiveBody)
        }

        data.todayDate = curDate
        return (data, true)
    }

    private static func archiveBlock(title: String, items: [TodoItem]) -> String {
        var lines: [String] = []
        lines.append("### \(title)")
        lines.append("")
        for item in items {
            lines.append(TodoParser.serializeItem(item))
        }
        return lines.joined(separator: "\n")
    }

    private static func prepend(_ block: String, to existing: String) -> String {
        if existing.isEmpty {
            return block
        }
        return block + "\n\n" + existing
    }
}

// MARK: - File I/O

enum TodoFile {
    static func load(from url: URL) -> TodoData {
        guard let text = try? String(contentsOf: url, encoding: .utf8) else {
            return TodoData.empty(todayDate: DateUtil.todayDateString())
        }
        return TodoParser.parse(text)
    }

    static func save(_ data: TodoData, to url: URL) throws {
        let text = TodoParser.serialize(data)
        let tmpURL = url.deletingLastPathComponent()
            .appendingPathComponent(".\(url.lastPathComponent).tmp-\(UUID().uuidString)")
        try text.write(to: tmpURL, atomically: true, encoding: .utf8)
        _ = try FileManager.default.replaceItemAt(url, withItemAt: tmpURL)
    }

    /// Loads from disk, applies rollover, persists if changed, returns the resulting data.
    static func loadWithRollover(from url: URL, now: Date = Date()) -> TodoData {
        let existed = FileManager.default.fileExists(atPath: url.path)
        if !existed {
            let fresh = TodoData.empty(todayDate: DateUtil.todayDateString(for: now))
            try? save(fresh, to: url)
            return fresh
        }
        let loaded = load(from: url)
        let (rolled, changed) = Rollover.apply(loaded, now: now)
        if changed {
            try? save(rolled, to: url)
        }
        return rolled
    }
}

// MARK: - File watcher

final class FileWatcher {
    private let url: URL
    private var source: DispatchSourceFileSystemObject?
    private var fd: Int32 = -1
    private let onChange: () -> Void
    private let queue = DispatchQueue(label: "io.github.uncertaintydeterminesyou4ndme.hibi.filewatcher")

    init(url: URL, onChange: @escaping () -> Void) {
        self.url = url
        self.onChange = onChange
    }

    func start() {
        stop()
        fd = open(url.path, O_EVTONLY)
        guard fd >= 0 else { return }
        let src = DispatchSource.makeFileSystemObjectSource(fileDescriptor: fd, eventMask: [.write, .rename, .delete], queue: queue)
        src.setEventHandler { [weak self] in
            guard let self = self else { return }
            DispatchQueue.main.async {
                self.onChange()
            }
            // Editors frequently save via rename, invalidating this fd. Re-establish the watch.
            self.queue.async {
                self.restart()
            }
        }
        src.setCancelHandler { [weak self] in
            guard let self = self else { return }
            if self.fd >= 0 {
                close(self.fd)
                self.fd = -1
            }
        }
        source = src
        src.resume()
    }

    private func restart() {
        DispatchQueue.main.async { [weak self] in
            self?.start()
        }
    }

    func stop() {
        source?.cancel()
        source = nil
        if fd >= 0 {
            close(fd)
            fd = -1
        }
    }

    deinit {
        stop()
    }
}

// MARK: - App State

@MainActor
final class AppState: ObservableObject {
    @Published var data: TodoData
    let fileURL: URL
    private var watcher: FileWatcher?

    /// The single globally-active tag filter, if any. In-memory only — cleared on
    /// relaunch. When set, the panel restricts all three tier sections to items
    /// carrying this tag; the menu bar badge count is unaffected (always unfiltered).
    @Published var activeTagFilter: String? = nil

    init(fileURL: URL = URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent("todos.md")) {
        self.fileURL = fileURL
        self.data = TodoFile.loadWithRollover(from: fileURL)
        self.watcher = FileWatcher(url: fileURL) { [weak self] in
            self?.reloadFromDisk()
        }
        self.watcher?.start()
    }

    /// True while a row is being edited in place. Reloads are deferred until the edit
    /// ends, so neither the file watcher nor an app-activation can yank the editor's
    /// row out from under it mid-typing.
    private var reloadSuspended = false
    private var reloadPending = false

    func reloadFromDisk() {
        guard !reloadSuspended else {
            reloadPending = true
            return
        }
        let fresh = TodoFile.loadWithRollover(from: fileURL)
        // Our own persist() trips the file watcher. Re-assigning `data` there would
        // mint new ids for every item and kill any in-flight edit/hover, so ignore a
        // reload that carries no actual content change.
        guard !fresh.sameContent(as: data) else { return }
        data = fresh
    }

    func suspendReload() {
        reloadSuspended = true
    }

    func resumeReload() {
        reloadSuspended = false
        if reloadPending {
            reloadPending = false
            reloadFromDisk()
        }
    }

    private func persist() {
        try? TodoFile.save(data, to: fileURL)
    }

    // MARK: Tier-generic actions

    func items(for tier: Tier) -> [TodoItem] {
        switch tier {
        case .today: return data.todayItems
        case .threeDay: return data.threeDayItems
        case .week: return data.weekItems
        }
    }

    private func withItems(_ tier: Tier, _ mutate: (inout [TodoItem]) -> Void) {
        switch tier {
        case .today: mutate(&data.todayItems)
        case .threeDay: mutate(&data.threeDayItems)
        case .week: mutate(&data.weekItems)
        }
    }

    func toggle(_ id: UUID, in tier: Tier) {
        withItems(tier) { items in
            if let idx = items.firstIndex(where: { $0.id == id }) {
                items[idx].done.toggle()
            }
        }
        persist()
    }

    func delete(_ id: UUID, in tier: Tier) {
        withItems(tier) { items in items.removeAll { $0.id == id } }
        persist()
    }

    func setQuadrant(_ id: UUID, _ quadrant: Quadrant?, in tier: Tier) {
        withItems(tier) { items in
            if let idx = items.firstIndex(where: { $0.id == id }) {
                items[idx].quadrant = quadrant
            }
        }
        persist()
    }

    func move(_ id: UUID, from: Tier, to: Tier) {
        guard from != to else { return }
        var moved: TodoItem?
        withItems(from) { items in
            if let idx = items.firstIndex(where: { $0.id == id }) {
                moved = items.remove(at: idx)
            }
        }
        guard let item = moved else { return }
        withItems(to) { items in items.append(item) }
        persist()
    }

    /// Rewrites an item from raw input using the *same* grammar as the quick-add field
    /// (`text #tag !n`), preserving `done` and the accumulated focus minutes. Returns
    /// false and changes nothing when the input parses to an empty task, so the caller
    /// can fall back to leaving the row as it was.
    @discardableResult
    func updateText(_ id: UUID, raw: String, in tier: Tier) -> Bool {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return false }
        let (afterSuffix, quadrant, minutes) = TodoParser.stripSuffixMarkers(trimmed)
        let (clean, tags) = TodoParser.extractTags(afterSuffix)
        guard !clean.isEmpty else { return false }
        var changed = false
        withItems(tier) { items in
            guard let idx = items.firstIndex(where: { $0.id == id }) else { return }
            items[idx].text = clean
            items[idx].tags = tags
            items[idx].quadrant = quadrant
            // `⏱Nm` is tracked by the timer, not typed by hand: an explicit marker in
            // the input overrides it, its absence leaves the running total alone.
            if let minutes = minutes { items[idx].focusMinutes = minutes }
            changed = true
        }
        if changed { persist() }
        return changed
    }

    func add(_ text: String, to tier: Tier) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        let (afterSuffix, quadrant, minutes) = TodoParser.stripSuffixMarkers(trimmed)
        let (clean, tags) = TodoParser.extractTags(afterSuffix)
        guard !clean.isEmpty else { return }
        withItems(tier) { items in items.append(TodoItem(text: clean, done: false, quadrant: quadrant, focusMinutes: minutes ?? 0, tags: tags)) }
        persist()
    }

    /// Credits `minutesToAdd` focus minutes onto the first item in `tier` whose text
    /// matches `text` exactly, then persists. Matching by text (rather than id) is
    /// intentional — see PomodoroSettlement. If no item matches, this is a silent no-op.
    func creditFocusMinutes(text: String, tier: Tier, minutesToAdd: Int) {
        guard minutesToAdd > 0 else { return }
        withItems(tier) { items in
            items = PomodoroSettlement.applyCredit(to: items, matchingText: text, minutesToAdd: minutesToAdd)
        }
        persist()
    }

    // MARK: Tag filter

    /// Sets the active tag filter directly (nil clears it).
    func setTagFilter(_ tag: String?) {
        activeTagFilter = tag
    }

    /// Clicking a chip toggles: re-clicking the currently active tag clears the
    /// filter, clicking a different tag switches the (single, global) filter to it.
    func toggleTagFilter(_ tag: String) {
        activeTagFilter = (activeTagFilter == tag) ? nil : tag
    }

    /// Items for `tier`, restricted to the active tag filter if one is set.
    func filteredItems(for tier: Tier) -> [TodoItem] {
        let all = items(for: tier)
        guard let tag = activeTagFilter else { return all }
        return all.filter { $0.tags.contains(tag) }
    }

    // MARK: Derived

    var todayRemainingCount: Int {
        data.todayItems.filter { !$0.done }.count
    }

    var todayCompletedCount: Int {
        data.todayItems.filter { $0.done }.count
    }

    var todayTotalCount: Int {
        data.todayItems.count
    }

    /// Today completed/total counts restricted to the active tag filter — used to
    /// drive the header progress ring while a filter is active. The menu bar badge
    /// always uses the unfiltered counts above.
    var filteredTodayCompletedCount: Int {
        filteredItems(for: .today).filter { $0.done }.count
    }

    var filteredTodayTotalCount: Int {
        filteredItems(for: .today).count
    }
}

// MARK: - Pomodoro

enum PomodoroPhase: Equatable {
    case idle
    case focus
    case paused
    /// Focus ran out and the break prompt is up, but the break isn't counting down yet:
    /// it only starts once the user acknowledges the prompt, so a missed reminder can't
    /// silently use up the break.
    case breakPending
    case breakTime
}

/// Pure functions for computing and applying focus-duration credit. Kept free of any
/// timer/UI state so they can be exercised directly in self-tests.
@MainActor
enum PomodoroSettlement {
    /// Minutes to credit for a focus session ending either by running out naturally
    /// (full `sessionMinutes`, the preset chosen for this session) or by being stopped
    /// early (the whole minutes actually ticked down, floored; a partial minute is
    /// discarded).
    static func minutesToCredit(elapsedSeconds: Int, completedNaturally: Bool, sessionMinutes: Int) -> Int {
        if completedNaturally {
            return sessionMinutes
        }
        return max(0, elapsedSeconds / 60)
    }

    /// Adds `minutesToAdd` onto `focusMinutes` of the first item in `items` whose text
    /// equals `matchingText`. Returns `items` unchanged if there is no match or
    /// `minutesToAdd` is not positive.
    static func applyCredit(to items: [TodoItem], matchingText text: String, minutesToAdd: Int) -> [TodoItem] {
        guard minutesToAdd > 0 else { return items }
        var result = items
        if let idx = result.firstIndex(where: { $0.text == text }) {
            result[idx].focusMinutes += minutesToAdd
        }
        return result
    }
}

/// Lets local notifications show as banners even while Hibi is the active app.
final class ForegroundNotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .sound])
    }
}

/// The once-a-second countdown, deliberately split out of `PomodoroState`.
///
/// While `remainingSeconds` was `@Published` on `PomodoroState`, every view observing
/// the timer re-ran its body each second — including `PanelView`, and with it every
/// row's `.contextMenu`. An open context menu was therefore torn down and rebuilt on
/// every tick: it flickered, and moving the mouse toward a submenu dismissed it before
/// it could be clicked. Only the two views that actually draw a countdown observe this
/// object; everything else observes `PomodoroState`, which now only publishes on real
/// state changes (start/pause/stop/phase).
@MainActor
final class PomodoroTicker: ObservableObject {
    @Published fileprivate(set) var remainingSeconds: Int = 0

    var timeString: String {
        let m = max(0, remainingSeconds) / 60
        let s = max(0, remainingSeconds) % 60
        return String(format: "%02d:%02d", m, s)
    }
}

@MainActor
final class PomodoroState: ObservableObject {
    static let focusPresets = [5, 15, 30, 40]
    static let breakMinutes = 10
    /// While the break prompt waits for an answer, the chime repeats on this interval,
    /// at most `breakReminderLimit` times — past that the user has likely stepped away.
    static let breakReminderInterval: TimeInterval = 60
    static let breakReminderLimit = 10

    private static let focusMinutesDefaultsKey = "hibi.focusMinutes"

    @Published private(set) var phase: PomodoroPhase = .idle {
        didSet {
            // Every exit from the prompt (start break, skip, stop, a new focus session
            // started from the panel) passes through here.
            if oldValue == .breakPending && phase != .breakPending {
                dismissBreakPrompt()
            }
        }
    }

    /// Owns the per-second countdown. Views that render it observe `ticker` directly.
    let ticker = PomodoroTicker()

    /// Forwards to `ticker`. Not `@Published` on purpose — see `PomodoroTicker`.
    private(set) var remainingSeconds: Int {
        get { ticker.remainingSeconds }
        set { ticker.remainingSeconds = newValue }
    }

    /// Last-chosen focus preset (minutes), persisted across launches so the next
    /// implicit `start()`/`startFocus()` (no explicit `minutes:`) reuses it.
    @Published var selectedFocusMinutes: Int

    /// Total minutes of the focus session currently running (or most recently run),
    /// used to compute elapsed time and to credit a naturally-completed session.
    private var sessionFocusMinutes: Int = 0

    /// Task this focus session is bound to (nil = free/unbound focus, v2 behavior: no
    /// duration is tracked). Cleared whenever a session ends (naturally or via stop).
    @Published private(set) var boundTaskId: UUID? = nil
    @Published private(set) var boundTaskTier: Tier? = nil
    @Published private(set) var boundTaskText: String? = nil

    /// Invoked to persist settled focus minutes onto the bound task: (text, tier, minutes).
    var onSettle: ((String, Tier, Int) -> Void)?

    private var timer: Timer?
    private var breakReminderTimer: Timer?
    private var breakRemindersPlayed = 0
    private let breakPrompt = BreakPrompt()
    private var notificationsAuthorized = false
    private let notificationDelegate = ForegroundNotificationDelegate()

    init() {
        let stored = UserDefaults.standard.integer(forKey: Self.focusMinutesDefaultsKey)
        selectedFocusMinutes = Self.focusPresets.contains(stored) ? stored : 40
        requestAuthorization()
    }

    private func requestAuthorization() {
        let center = UNUserNotificationCenter.current()
        // Without a delegate, macOS suppresses banners while the app is frontmost
        // (i.e. whenever the menu bar panel is open), so a focus session ending
        // while the user is watching the countdown would show nothing.
        center.delegate = notificationDelegate
        center.requestAuthorization(options: [.alert, .sound]) { [weak self] granted, _ in
            Task { @MainActor in
                self?.notificationsAuthorized = granted
            }
        }
    }

    var timeString: String { ticker.timeString }

    /// Starts a free focus session, not bound to any task (v2 behavior — no duration
    /// tracked). If a session is already running for another (bound or free) task, it
    /// is settled first.
    func start(minutes: Int? = nil) {
        beginFocus(taskId: nil, tier: nil, text: nil, minutes: minutes)
    }

    /// Starts a focus session bound to a specific task. If a session is already
    /// running, it is settled first (see PomodoroSettlement).
    func startFocus(taskId: UUID, tier: Tier, text: String, minutes: Int? = nil) {
        beginFocus(taskId: taskId, tier: tier, text: text, minutes: minutes)
    }

    private func beginFocus(taskId: UUID?, tier: Tier?, text: String?, minutes: Int?) {
        if phase == .focus || phase == .paused {
            settle(naturalCompletion: false)
        }
        let chosenMinutes = minutes ?? selectedFocusMinutes
        selectedFocusMinutes = chosenMinutes
        UserDefaults.standard.set(chosenMinutes, forKey: Self.focusMinutesDefaultsKey)
        sessionFocusMinutes = chosenMinutes
        boundTaskId = taskId
        boundTaskTier = tier
        boundTaskText = text
        phase = .focus
        remainingSeconds = chosenMinutes * 60
        startTimer()
    }

    func pause() {
        guard phase == .focus else { return }
        phase = .paused
        timer?.invalidate()
        timer = nil
    }

    func resume() {
        guard phase == .paused else { return }
        phase = .focus
        startTimer()
    }

    func stop() {
        if phase == .focus || phase == .paused {
            settle(naturalCompletion: false)
        }
        phase = .idle
        remainingSeconds = 0
        clearBinding()
        timer?.invalidate()
        timer = nil
    }

    func startBreak() {
        guard phase == .breakPending else { return }
        phase = .breakTime
        remainingSeconds = Self.breakMinutes * 60
        startTimer()
    }

    func skip() {
        guard phase == .breakTime || phase == .breakPending else { return }
        stop()
    }

    private func clearBinding() {
        boundTaskId = nil
        boundTaskTier = nil
        boundTaskText = nil
    }

    /// Settles the current bound session's focus minutes (if any) via `onSettle`.
    /// No-op for a free (unbound) session. Does not touch phase/timer state.
    private func settle(naturalCompletion: Bool) {
        guard let text = boundTaskText, let tier = boundTaskTier else { return }
        let elapsed = sessionFocusMinutes * 60 - remainingSeconds
        let minutes = PomodoroSettlement.minutesToCredit(elapsedSeconds: elapsed, completedNaturally: naturalCompletion, sessionMinutes: sessionFocusMinutes)
        if minutes > 0 {
            onSettle?(text, tier, minutes)
        }
    }

    private func startTimer() {
        timer?.invalidate()
        let t = Timer(timeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.tick()
            }
        }
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    private func tick() {
        guard remainingSeconds > 0 else { return }
        remainingSeconds -= 1
        if remainingSeconds == 0 {
            switch phase {
            case .focus:
                settle(naturalCompletion: true)
                clearBinding()
                timer?.invalidate()
                timer = nil
                phase = .breakPending
                showBreakPrompt()
            case .breakTime:
                notifyAndChime(title: "休息结束", body: "开始下一轮吧")
                stop()
            default:
                break
            }
        }
    }

    /// The focus-end reminder. A notification banner was easy to miss — it sits in a
    /// corner and leaves after a few seconds — so this is a centered panel that stays
    /// until answered, plus a chime that repeats while it waits.
    private func showBreakPrompt() {
        NSSound(named: "Glass")?.play()
        breakPrompt.show(focusMinutes: sessionFocusMinutes, breakMinutes: Self.breakMinutes,
                         onStart: { [weak self] in self?.startBreak() },
                         onSkip: { [weak self] in self?.skip() })
        breakRemindersPlayed = 0
        let t = Timer(timeInterval: Self.breakReminderInterval, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.remindBreak()
            }
        }
        RunLoop.main.add(t, forMode: .common)
        breakReminderTimer = t
    }

    private func remindBreak() {
        guard phase == .breakPending, breakRemindersPlayed < Self.breakReminderLimit else {
            breakReminderTimer?.invalidate()
            breakReminderTimer = nil
            return
        }
        breakRemindersPlayed += 1
        NSSound(named: "Glass")?.play()
        breakPrompt.bringToFront()
    }

    private func dismissBreakPrompt() {
        breakReminderTimer?.invalidate()
        breakReminderTimer = nil
        breakPrompt.dismiss()
    }

    private func notifyAndChime(title: String, body: String) {
        if notificationsAuthorized {
            let content = UNMutableNotificationContent()
            content.title = title
            content.body = body
            content.sound = .default
            let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
            UNUserNotificationCenter.current().add(request)
        }
        NSSound(named: "Glass")?.play()
    }
}

// MARK: - UI

/// Grok-style palette: no hue anywhere, high contrast, 1px rules, 4px radii.
/// Hierarchy comes from ink density and whitespace, never from colored fills.
struct Theme {
    let bg: Color          // panel ground (opaque — the menu bar window's own material is covered)
    let raised: Color      // hovered row, input well
    let text: Color
    let secondary: Color
    let faint: Color       // tags, durations, section labels
    let line: Color        // hairline rules and unchecked box outlines

    static let radius: CGFloat = 4

    static func of(_ scheme: ColorScheme) -> Theme {
        scheme == .dark ? .dark : .light
    }

    static let light = Theme(
        bg:        Color(red: 1.00, green: 1.00, blue: 1.00),
        raised:    Color(red: 0.949, green: 0.949, blue: 0.953),
        text:      Color(red: 0.055, green: 0.055, blue: 0.063),
        secondary: Color(red: 0.435, green: 0.435, blue: 0.451),
        faint:     Color(red: 0.643, green: 0.643, blue: 0.659),
        line:      Color(red: 0.898, green: 0.898, blue: 0.906)
    )

    static let dark = Theme(
        bg:        Color(red: 0.039, green: 0.039, blue: 0.039),
        raised:    Color(red: 0.110, green: 0.110, blue: 0.114),
        text:      Color(red: 0.957, green: 0.957, blue: 0.961),
        secondary: Color(red: 0.541, green: 0.541, blue: 0.557),
        faint:     Color(red: 0.361, green: 0.361, blue: 0.376),
        line:      Color(red: 0.153, green: 0.153, blue: 0.161)
    )

    /// Quadrant priority as ink density on the row's leading rule: q1 solid, fading to
    /// nearly nothing at q4, absent when unset. Reads as darkness rather than hue, so
    /// it survives both color schemes and colorblindness without a second palette.
    func rail(for quadrant: Quadrant?) -> Color {
        switch quadrant {
        case .q1: return text
        case .q2: return text.opacity(0.42)
        case .q3: return text.opacity(0.24)
        case .q4: return text.opacity(0.12)
        case .none: return .clear
        }
    }
}

/// The app icon's cat head as one shared piece of geometry — wide ellipse head,
/// quadratic-rounded ear tips, negative-space eyes, authored on a 24×24 canvas.
/// Always a template image, so every surface (menu bar, Control Center, panel)
/// tints it with its own ink and the panel's no-hue discipline stays intact.
enum CatGlyph {
    static func image(size: CGFloat) -> NSImage {
        let image = NSImage(size: NSSize(width: size, height: size), flipped: true) { rect in
            guard let ctx = NSGraphicsContext.current?.cgContext else { return false }
            ctx.scaleBy(x: rect.width / 24, y: rect.height / 24)
            ctx.setFillColor(NSColor.black.cgColor)

            // Ears: triangles with quadratic-rounded tips.
            for (outer, tip, inner) in [(NSPoint(x: 2.8, y: 10.6), NSPoint(x: 5.4, y: 2.4), NSPoint(x: 10.8, y: 8.0)),
                                        (NSPoint(x: 21.2, y: 10.6), NSPoint(x: 18.6, y: 2.4), NSPoint(x: 13.2, y: 8.0))] {
                let d: CGFloat = 1.7
                func toward(_ from: NSPoint, _ to: NSPoint) -> NSPoint {
                    let dx = to.x - from.x, dy = to.y - from.y
                    let len = (dx * dx + dy * dy).squareRoot()
                    return NSPoint(x: from.x + dx / len * d, y: from.y + dy / len * d)
                }
                let ear = NSBezierPath()
                ear.move(to: outer)
                ear.line(to: toward(tip, outer))
                ear.curve(to: toward(tip, inner), controlPoint: tip)
                ear.line(to: inner)
                ear.close()
                ear.fill()
            }
            // Head: wide ellipse.
            ctx.fillEllipse(in: CGRect(x: 2.4, y: 8.2, width: 19.2, height: 13.6))

            // Eyes: negative-space vertical rounded rects.
            ctx.setBlendMode(.destinationOut)
            for x in [7.7, 14.3] as [CGFloat] {
                NSBezierPath(roundedRect: NSRect(x: x, y: 12.6, width: 2.0, height: 4.2),
                             xRadius: 1.0, yRadius: 1.0).fill()
            }
            return true
        }
        image.isTemplate = true
        return image
    }

    static let menuBar = image(size: 18)
    static let emptyState = image(size: 30)
    static let help = image(size: 12)
}

extension View {
    /// `onChange(of:perform:)` is the only spelling available on our macOS 13.0
    /// deployment floor, and is deprecated from 14 on. This picks the modern
    /// two-parameter form when it exists; the deprecated call is confined to a
    /// deprecated shim, which is how Swift lets it compile without a warning.
    @ViewBuilder
    func onValueChange<V: Equatable>(of value: V, perform action: @escaping (V) -> Void) -> some View {
        if #available(macOS 14.0, *) {
            onChange(of: value) { _, newValue in action(newValue) }
        } else {
            legacyOnChange(of: value, perform: action)
        }
    }

    @available(macOS, deprecated: 14.0, message: "Shim only — callers should use onValueChange.")
    private func legacyOnChange<V: Equatable>(of value: V, perform action: @escaping (V) -> Void) -> some View {
        onChange(of: value, perform: action)
    }
}

/// 1px rule. Replaces `Divider()`, whose thickness and (vibrancy-derived) color can't
/// be pinned down inside a menu bar window.
struct Hairline: View {
    let color: Color
    var body: some View {
        Rectangle().fill(color).frame(height: 1)
    }
}

/// Height math for the panel's scrolling list area.
///
/// The list *must* be given a definite height. Inside `MenuBarExtra(.window)`
/// SwiftUI proposes an unspecified height to the content, and a `ScrollView` has
/// no intrinsic height of its own — it resolves to 0. A bare
/// `.frame(maxHeight:)` is only a ceiling, so nothing stops the collapse: the
/// panel then opens showing the header, quick-add bar and toolbar with *no
/// visible list*, even though the rows were laid out normally inside it (a
/// 305x212 DocumentView sitting in a 320x0 HostingScrollView). Feeding the
/// measured content height back in as a definite `.frame(height:)` gives the
/// window something concrete to size itself to.
enum PanelLayout {
    /// Tallest the list may grow before it starts scrolling instead.
    static let maxListHeight: CGFloat = 420

    /// Floor used before the first measurement lands (and as a guard against a
    /// degenerate 0). Roughly the height of the "今天没有待办" empty state, so
    /// the pre-measurement frame is never taller than the real content.
    static let minListHeight: CGFloat = 64

    static func listHeight(contentHeight: CGFloat) -> CGFloat {
        guard contentHeight > 0 else { return minListHeight }
        return min(max(contentHeight, minListHeight), maxListHeight)
    }
}

/// Carries the measured height of the list content out of the `ScrollView`.
private struct ListContentHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        value = max(value, nextValue())
    }
}

/// Carries the measured height of the whole panel out of the root layout, so the
/// window can be resized to fit it. `MenuBarExtra(.window)` frames the panel from
/// the *first* layout pass — when `listContentHeight` still reads 0 and the list
/// is only `minListHeight` tall — and never revisits the frame when the real
/// measurement lands. Without an explicit resize, the first open after launch
/// clips the bottom of the list, the quick-add bar and the toolbar; every later
/// open looks fine only because the retained `@State` still holds the last
/// measured height.
private struct PanelHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        value = max(value, nextValue())
    }
}

/// Reads the `NSWindow` a view is hosted in. `MenuBarExtra` exposes no handle to
/// its panel, so the view hierarchy is the only way up.
private struct WindowAccessor: NSViewRepresentable {
    let onResolve: (NSWindow?) -> Void

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async { onResolve(view.window) }
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        DispatchQueue.main.async { onResolve(nsView.window) }
    }
}

struct PanelView: View {
    @ObservedObject var appState: AppState
    @ObservedObject var pomodoro: PomodoroState
    @Environment(\.colorScheme) private var colorScheme
    @State private var quickAddText: String = ""
    @FocusState private var quickAddFocused: Bool
    @State private var eventMonitor: Any?
    @State private var showHelp: Bool = false
    @State private var listContentHeight: CGFloat = 0
    @State private var panelHeight: CGFloat = 0
    @State private var panelWindow: NSWindow?
    @State private var editingId: UUID?
    @State private var editingTier: Tier = .today
    /// Name of whatever app currently owns `.md`, resolved once when the panel opens.
    /// Shown in the Open File tooltip so a slow handler (Xcode is a common default
    /// owner of `.md`) explains itself instead of looking like a dead button.
    @State private var openerName: String?

    private var theme: Theme { Theme.of(colorScheme) }

    /// Resizes the panel window to the measured content height. The panel hangs
    /// below the menu bar, so the top edge stays pinned and the window grows
    /// downward. No-op until both the window and a real measurement exist.
    private func fitPanelWindow() {
        guard let window = panelWindow, panelHeight > 0 else { return }
        let size = NSSize(width: 340, height: panelHeight)
        guard abs(window.frame.width - size.width) > 0.5
                || abs(window.frame.height - size.height) > 0.5 else { return }
        var frame = window.frame
        frame.origin.y = frame.maxY - size.height
        frame.size = size
        window.setFrame(frame, display: true)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            Hairline(color: theme.line)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    if appState.activeTagFilter != nil {
                        filteredSectionsView
                    } else {
                        todaySection
                        if !appState.data.threeDayItems.isEmpty {
                            Hairline(color: theme.line)
                            threeDaySection
                        }
                        if !appState.data.weekItems.isEmpty {
                            Hairline(color: theme.line)
                            weekSection
                        }
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 8)
                .background(
                    GeometryReader { proxy in
                        Color.clear.preference(key: ListContentHeightKey.self, value: proxy.size.height)
                    }
                )
            }
            .frame(height: PanelLayout.listHeight(contentHeight: listContentHeight))
            .onPreferenceChange(ListContentHeightKey.self) { height in
                listContentHeight = height
            }
            Hairline(color: theme.line)
            quickAddBar
            Hairline(color: theme.line)
            bottomToolbar
        }
        .frame(width: 340)
        .background(theme.bg)
        .background(
            GeometryReader { proxy in
                Color.clear.preference(key: PanelHeightKey.self, value: proxy.size.height)
            }
        )
        .background(WindowAccessor { window in
            panelWindow = window
            fitPanelWindow()
        })
        .onPreferenceChange(PanelHeightKey.self) { height in
            panelHeight = height
            fitPanelWindow()
        }
        .onAppear {
            appState.reloadFromDisk()
            installEventMonitor()
            openerName = NSWorkspace.shared.urlForApplication(toOpen: appState.fileURL)
                .flatMap { FileManager.default.displayName(atPath: $0.path) }
        }
        .onDisappear {
            cancelEdit()
            removeEventMonitor()
        }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            appState.reloadFromDisk()
        }
    }

    // MARK: Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Text("今日")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(theme.text)
                Text(DateUtil.headerDisplay(for: appState.data.todayDate))
                    .font(.system(size: 11))
                    .foregroundStyle(theme.secondary)
                Spacer(minLength: 8)
                if appState.activeTagFilter != nil {
                    ProgressRing(completed: appState.filteredTodayCompletedCount, total: appState.filteredTodayTotalCount, theme: theme)
                } else {
                    ProgressRing(completed: appState.todayCompletedCount, total: appState.todayTotalCount, theme: theme)
                }
            }
            HStack(spacing: 8) {
                if let tag = appState.activeTagFilter {
                    filterBar(tag)
                }
                Spacer(minLength: 0)
                PomodoroControl(pomodoro: pomodoro, ticker: pomodoro.ticker, theme: theme)
            }
        }
        .padding(.horizontal, 14)
        .padding(.top, 11)
        .padding(.bottom, 9)
    }

    /// The one inverted block in the panel — the active filter is the only state that
    /// warrants shouting, and inverting ink is how it shouts without introducing a hue.
    private func filterBar(_ tag: String) -> some View {
        HStack(spacing: 5) {
            Text("#\(tag)")
                .font(.system(size: 11, weight: .medium))
            Button(action: { appState.setTagFilter(nil) }) {
                Image(systemName: "xmark")
                    .font(.system(size: 8, weight: .bold))
            }
            .buttonStyle(.plain)
        }
        .foregroundStyle(theme.bg)
        .padding(.horizontal, 7)
        .padding(.vertical, 3)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(theme.text))
    }

    // MARK: Sections

    private var todaySection: some View {
        Group {
            if appState.data.todayItems.isEmpty {
                emptyState("今天没有待办")
            } else {
                sectionView(tier: .today, items: appState.data.todayItems, title: nil,
                            moveTargets: [(.threeDay, "三日"), (.week, "本周")])
            }
        }
    }

    /// Empty lists are the panel's natural brand moment: the shared cat mark in faint
    /// ink above the placeholder line, instead of bare text.
    private func emptyState(_ text: String) -> some View {
        VStack(spacing: 7) {
            Image(nsImage: CatGlyph.emptyState)
            Text(text)
                .font(.system(size: 12))
        }
        .foregroundStyle(theme.faint)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 16)
    }

    private var threeDaySection: some View {
        sectionView(tier: .threeDay, items: appState.data.threeDayItems, title: "三日",
                    moveTargets: [(.today, "今日"), (.week, "本周")])
    }

    private var weekSection: some View {
        sectionView(tier: .week, items: appState.data.weekItems, title: "本周",
                    moveTargets: [(.today, "今日"), (.threeDay, "三日")])
    }

    /// While a tag filter is active, all three tiers are restricted to matching items
    /// and a tier's section is hidden entirely (not shown with a placeholder) when it
    /// has no matches. If none of the three tiers match, a single centered placeholder
    /// is shown instead.
    private var filteredSectionsView: some View {
        let specs: [(Tier, String, [(Tier, String)])] = [
            (.today, "今日", [(.threeDay, "三日"), (.week, "本周")]),
            (.threeDay, "三日", [(.today, "今日"), (.week, "本周")]),
            (.week, "本周", [(.today, "今日"), (.threeDay, "三日")]),
        ]
        let visible = specs.compactMap { tier, title, targets -> (Tier, String, [(Tier, String)], [TodoItem])? in
            let items = appState.filteredItems(for: tier)
            return items.isEmpty ? nil : (tier, title, targets, items)
        }
        return Group {
            if visible.isEmpty {
                emptyState("没有匹配的任务")
            } else {
                ForEach(Array(visible.enumerated()), id: \.offset) { idx, entry in
                    if idx > 0 { Hairline(color: theme.line) }
                    sectionView(tier: entry.0, items: entry.3, title: entry.1, moveTargets: entry.2)
                }
            }
        }
    }

    private func sectionView(tier: Tier, items: [TodoItem], title: String?, moveTargets: [(Tier, String)]) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            if let title = title {
                Text(title)
                    .font(.system(size: 10.5, weight: .semibold))
                    .foregroundStyle(theme.faint)
                    .padding(.horizontal, 10)
                    .padding(.top, 2)
                    .padding(.bottom, 3)
            }
            ForEach(TodoSort.sorted(items)) { item in
                TodoRow(
                    item: item,
                    theme: theme,
                    isFocusing: pomodoro.phase == .focus && pomodoro.boundTaskId == item.id,
                    isEditing: editingId == item.id,
                    onToggle: { appState.toggle(item.id, in: tier) },
                    onDelete: { appState.delete(item.id, in: tier) },
                    onStartFocus: { pomodoro.startFocus(taskId: item.id, tier: tier, text: item.text) },
                    onTagTap: { tag in appState.toggleTagFilter(tag) },
                    onBeginEdit: { beginEdit(item, in: tier) },
                    onCommitEdit: { text in commitEdit(text) }
                )
                .contextMenu {
                    Button("编辑") { beginEdit(item, in: tier) }
                    Menu("开始专注") {
                        ForEach(PomodoroState.focusPresets, id: \.self) { m in
                            Button("\(m) 分钟") { pomodoro.startFocus(taskId: item.id, tier: tier, text: item.text, minutes: m) }
                        }
                    }
                    Menu("象限") {
                        Button("重要且紧急") { appState.setQuadrant(item.id, .q1, in: tier) }
                        Button("重要不紧急") { appState.setQuadrant(item.id, .q2, in: tier) }
                        Button("紧急不重要") { appState.setQuadrant(item.id, .q3, in: tier) }
                        Button("不重要不紧急") { appState.setQuadrant(item.id, .q4, in: tier) }
                        Divider()
                        Button("清除标记") { appState.setQuadrant(item.id, nil, in: tier) }
                    }
                    Menu("移到") {
                        ForEach(moveTargets, id: \.1) { target in
                            Button(target.1) { appState.move(item.id, from: tier, to: target.0) }
                        }
                    }
                    Button("删除") { appState.delete(item.id, in: tier) }
                }
            }
        }
    }

    // MARK: In-place editing

    /// Editing runs against the item's *raw* form (`text #tag !n`) — the same grammar
    /// the quick-add field accepts — so there's only one syntax to learn. Reloads are
    /// suspended for the duration: `persist()` echoes back through the file watcher,
    /// and re-parsing would mint a new `id` for the row being edited.
    private func beginEdit(_ item: TodoItem, in tier: Tier) {
        guard editingId != item.id else { return }
        if editingId != nil { cancelEdit() }
        appState.suspendReload()
        editingTier = tier
        editingId = item.id
    }

    private func commitEdit(_ text: String) {
        guard let id = editingId else { return }
        appState.updateText(id, raw: text, in: editingTier)
        finishEdit()
    }

    private func cancelEdit() {
        guard editingId != nil else { return }
        finishEdit()
    }

    private func finishEdit() {
        editingId = nil
        appState.resumeReload()
    }

    // MARK: Quick add

    private var quickAddPlaceholder: String {
        if let tag = appState.activeTagFilter { return "添加到 #\(tag)…" }
        return "添加待办…"
    }

    /// Adds `quickAddText` to `tier`. While a tag filter is active, the active tag is
    /// appended to the raw input before parsing so the new task automatically carries
    /// it (duplicate tags are harmless — `extractTags` de-dupes by name).
    private func submitQuickAdd(to tier: Tier) {
        var raw = quickAddText
        if let tag = appState.activeTagFilter {
            raw += " #\(tag)"
        }
        appState.add(raw, to: tier)
        quickAddText = ""
        quickAddFocused = true
    }

    private var quickAddBar: some View {
        HStack(spacing: 9) {
            Image(systemName: "plus")
                .font(.system(size: 11, weight: .medium))
                .foregroundStyle(theme.faint)
            TextField(quickAddPlaceholder, text: $quickAddText)
                .textFieldStyle(.plain)
                .font(.system(size: 13))
                .foregroundStyle(theme.text)
                .focused($quickAddFocused)
                .onSubmit {
                    submitQuickAdd(to: .today)
                }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }

    private func installEventMonitor() {
        guard eventMonitor == nil else { return }
        eventMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            // Esc leaves an in-place edit without writing it back.
            if editingId != nil, event.keyCode == 53 {
                cancelEdit()
                return nil
            }
            if quickAddFocused, event.keyCode == 36 {
                if event.modifierFlags.contains(.command) {
                    submitQuickAdd(to: .week)
                    return nil
                } else if event.modifierFlags.contains(.option) {
                    submitQuickAdd(to: .threeDay)
                    return nil
                }
            }
            return event
        }
    }

    private func removeEventMonitor() {
        if let m = eventMonitor {
            NSEvent.removeMonitor(m)
            eventMonitor = nil
        }
    }

    // MARK: Bottom toolbar

    /// Opens `~/todos.md` in whatever app owns `.md`, *activating* it. Without
    /// `activates`, a slow-launching handler (Xcode is a common default owner of `.md`)
    /// comes up behind everything and the click looks like it did nothing. If there's
    /// no handler at all, or the launch fails, fall back to revealing the file in
    /// Finder so the click always produces something visible.
    private func openTodoFile() {
        let url = appState.fileURL
        guard let app = NSWorkspace.shared.urlForApplication(toOpen: url) else {
            NSWorkspace.shared.activateFileViewerSelecting([url])
            return
        }
        let config = NSWorkspace.OpenConfiguration()
        config.activates = true
        NSWorkspace.shared.open([url], withApplicationAt: app, configuration: config) { _, error in
            guard error != nil else { return }
            DispatchQueue.main.async {
                NSWorkspace.shared.activateFileViewerSelecting([url])
            }
        }
    }

    private var bottomToolbar: some View {
        HStack(spacing: 10) {
            IconToolbarButton(systemImage: "doc.plaintext", theme: theme, action: openTodoFile)
                .help(openerName.map { "用 \($0) 打开 \(appState.fileURL.path)" }
                      ?? "在 Finder 中显示 \(appState.fileURL.path)")
                .contextMenu {
                    Button("在 Finder 中显示") {
                        NSWorkspace.shared.activateFileViewerSelecting([appState.fileURL])
                    }
                    Button("拷贝路径") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(appState.fileURL.path, forType: .string)
                    }
                }

            Button(action: { showHelp.toggle() }) {
                Image(systemName: "questionmark.circle")
                    .font(.system(size: 11.5))
            }
            .buttonStyle(.plain)
            .foregroundStyle(theme.secondary)
            .popover(isPresented: $showHelp, arrowEdge: .top) {
                helpContent
            }

            Spacer()

            IconToolbarButton(systemImage: "power", theme: theme, action: { NSApp.terminate(nil) })
                .help("退出")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
    }

    // MARK: Help popover

    private var helpContent: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(spacing: 5) {
                Image(nsImage: CatGlyph.help)
                Text("Hibi")
                    .font(.system(size: 11.5, weight: .semibold))
            }
            .foregroundStyle(theme.secondary)
            helpSection(title: "添加", lines: [
                "↩ 加到今日 · ⌥↩ 三日 · ⌘↩ 本周",
                "任务结尾写 !1~!4 标象限",
                "#项目名 打标签，点标签可筛选",
            ])
            helpSection(title: "修改", lines: [
                "双击任务在面板里直接改，写法和添加时一样",
                "↩ 保存 · esc 放弃 · 点别处也会保存",
            ])
            helpSection(title: "象限", lines: [
                "任务左侧竖线越深优先级越高：!1 最深，!4 最浅",
                "右键任务可改象限 / 移动层级",
            ])
            helpSection(title: "专注", lines: [
                "悬停任务点播放键为它计时，右键可选 5/15/30/40 分钟",
                "专注时长自动累加到任务上，到时发系统通知 + 提示音",
            ])
        }
        .padding(12)
        .frame(width: 280, alignment: .leading)
        .background(theme.bg)
    }

    private func helpSection(title: String, lines: [String]) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(.system(size: 11.5, weight: .semibold))
                .foregroundStyle(theme.text)
            ForEach(lines, id: \.self) { line in
                Text(line)
                    .font(.system(size: 11))
                    .foregroundStyle(theme.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Icon-only toolbar button that darkens to full ink on hover — the whole hover
/// vocabulary of the panel, no fills or outlines.
struct IconToolbarButton: View {
    let systemImage: String
    let theme: Theme
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.system(size: 11.5))
                .frame(width: 16, height: 13)
        }
        .buttonStyle(.plain)
        .foregroundStyle(hovering ? theme.text : theme.secondary)
        .onHover { hovering = $0 }
    }
}

struct TodoRow: View {
    let item: TodoItem
    let theme: Theme
    let isFocusing: Bool
    let isEditing: Bool
    let onToggle: () -> Void
    let onDelete: () -> Void
    let onStartFocus: () -> Void
    let onTagTap: (String) -> Void
    let onBeginEdit: () -> Void
    let onCommitEdit: (String) -> Void

    /// Kept local rather than lifted into `PanelView`: hovering a row must not
    /// invalidate its siblings, or moving the mouse off a row toward its own open
    /// context menu would rebuild that menu and dismiss it.
    @State private var isHovering = false

    /// Minutes as `40m` / `1h25m`. No clock glyph — the unit carries it.
    private func formattedFocusDuration(_ minutes: Int) -> String {
        guard minutes >= 60 else { return "\(minutes)m" }
        let h = minutes / 60
        let m = minutes % 60
        return m > 0 ? "\(h)h\(m)m" : "\(h)h"
    }

    /// Width reserved for the trailing action buttons. Always laid out (just
    /// transparent when not hovering) so revealing them never re-wraps the text
    /// and shifts the row.
    private static let actionSlotWidth: CGFloat = 36

    private var rowBackground: Color {
        if isEditing || isHovering { return theme.raised }
        return .clear
    }

    /// A stroked `Circle` is hit-tested on its 1pt ring only, so without an explicit
    /// content shape a click in the middle of the box fell straight through. The
    /// shape stays 13pt; the clickable area is a 22pt square around it.
    private var checkbox: some View {
        Button(action: onToggle) {
            ZStack {
                if item.done {
                    Image(systemName: "checkmark")
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(theme.secondary)
                } else {
                    Circle()
                        .strokeBorder(isHovering ? theme.secondary : theme.faint, lineWidth: 1.2)
                        .frame(width: 13, height: 13)
                }
            }
            .frame(width: 22, height: 22)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .frame(width: 13, height: 13)
    }

    private var content: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(item.text)
                .font(.system(size: 13))
                .strikethrough(item.done, color: theme.faint)
                .foregroundStyle(item.done ? theme.faint : theme.text)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)

            ForEach(item.tags, id: \.self) { tag in
                TagChip(tag: tag, theme: theme, onTap: { onTagTap(tag) })
            }

            if item.focusMinutes > 0 {
                Text(formattedFocusDuration(item.focusMinutes))
                    .font(.system(size: 11))
                    .foregroundStyle(theme.faint)
                    .monospacedDigit()
            }

            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
        // Double-click on the text only. On the whole row it also sat over the checkbox
        // and the action buttons, where a two-click recognizer gets in the way of
        // single clicks.
        .onTapGesture(count: 2) { onBeginEdit() }
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 9) {
            checkbox

            if isEditing {
                RowEditor(initial: item.editableText, theme: theme, onCommit: onCommitEdit)
            } else {
                content
            }

            HStack(spacing: 7) {
                if isFocusing {
                    Image(systemName: "timer")
                        .foregroundStyle(theme.text)
                } else if !item.done && !isEditing {
                    Button(action: onStartFocus) {
                        Image(systemName: "play")
                            .foregroundStyle(theme.secondary)
                    }
                    .buttonStyle(.plain)
                    .opacity(isHovering ? 1 : 0)
                }
                if !isEditing {
                    Button(action: onDelete) {
                        Image(systemName: "xmark")
                            .foregroundStyle(theme.secondary)
                    }
                    .buttonStyle(.plain)
                    .opacity(isHovering ? 1 : 0)
                }
            }
            .font(.system(size: 10.5, weight: .medium))
            .frame(width: Self.actionSlotWidth, alignment: .trailing)
        }
        .padding(.leading, 10)
        .padding(.trailing, 8)
        .padding(.vertical, 5)
        .background(
            RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                .fill(rowBackground)
        )
        .overlay(alignment: .leading) {
            // Quadrant rail. Completed work has no priority left, so its rail goes away.
            RoundedRectangle(cornerRadius: 1, style: .continuous)
                .fill(item.done ? Color.clear : theme.rail(for: item.quadrant))
                .frame(width: 2)
                .padding(.vertical, 3)
        }
        .contentShape(Rectangle())
        .onHover { hovering in
            isHovering = hovering
        }
    }
}

/// The in-place text field. Its draft lives here, not in `PanelView`: as panel state,
/// every keystroke re-ran the whole panel body (all rows, all context menus), which
/// is what made typing feel sluggish.
private struct RowEditor: View {
    let theme: Theme
    let onCommit: (String) -> Void
    @State private var text: String
    @FocusState private var focused: Bool

    init(initial: String, theme: Theme, onCommit: @escaping (String) -> Void) {
        _text = State(initialValue: initial)
        self.theme = theme
        self.onCommit = onCommit
    }

    var body: some View {
        TextField("", text: $text)
            .textFieldStyle(.plain)
            .font(.system(size: 13))
            .foregroundStyle(theme.text)
            .focused($focused)
            .onSubmit { onCommit(text) }
            // Focus has to be claimed a tick after the field is installed; setting it
            // inside the same layout pass is dropped.
            .onAppear { DispatchQueue.main.async { focused = true } }
            .onValueChange(of: focused) { isFocused in
                // Clicking elsewhere in the panel commits, matching the quick-add field's
                // "typing is never silently thrown away" behavior.
                if !isFocused { onCommit(text) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// A tag as plain `#name` text, clickable to filter. Monochrome by design: eight
/// pastel chips competing with the quadrant rails was the single biggest source of
/// visual noise in the old panel.
struct TagChip: View {
    let tag: String
    let theme: Theme
    let onTap: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: onTap) {
            Text("#\(tag)")
                .font(.system(size: 11))
                .foregroundStyle(hovering ? theme.secondary : theme.faint)
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}

struct ProgressRing: View {
    let completed: Int
    let total: Int
    let theme: Theme

    private var fraction: CGFloat {
        guard total > 0 else { return 0 }
        return CGFloat(completed) / CGFloat(total)
    }

    var body: some View {
        HStack(spacing: 6) {
            ZStack {
                Circle()
                    .stroke(theme.line, lineWidth: 1.5)
                Circle()
                    .trim(from: 0, to: fraction)
                    .stroke(theme.text, style: StrokeStyle(lineWidth: 1.5, lineCap: .butt))
                    .rotationEffect(.degrees(-90))
            }
            .frame(width: 12, height: 12)

            Text("\(completed)/\(total)")
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(theme.secondary)
        }
    }
}

struct PomodoroControl: View {
    @ObservedObject var pomodoro: PomodoroState
    @ObservedObject var ticker: PomodoroTicker
    let theme: Theme

    /// Truncates a bound task's name to roughly 12 characters for the header display.
    private func truncatedTaskName(_ text: String) -> String {
        let maxLen = 12
        guard text.count > maxLen else { return text }
        return String(text.prefix(maxLen)) + "…"
    }

    var body: some View {
        HStack(spacing: 7) {
            switch pomodoro.phase {
            case .idle:
                Menu {
                    ForEach(PomodoroState.focusPresets, id: \.self) { m in
                        Button("\(m) 分钟") { pomodoro.start(minutes: m) }
                    }
                } label: {
                    HStack(spacing: 3) {
                        Image(systemName: "play")
                        Image(systemName: "chevron.down")
                            .font(.system(size: 7, weight: .bold))
                    }
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
                .foregroundStyle(theme.secondary)
                .help("开始专注")

            case .focus:
                if let taskText = pomodoro.boundTaskText {
                    Text(truncatedTaskName(taskText))
                        .lineLimit(1)
                        .foregroundStyle(theme.secondary)
                }
                Text(ticker.timeString)
                    .monospacedDigit()
                    .foregroundStyle(theme.text)
                Button(action: { pomodoro.pause() }) {
                    Image(systemName: "pause")
                }
                .buttonStyle(.plain)
                Button(action: { pomodoro.stop() }) {
                    Image(systemName: "stop")
                }
                .buttonStyle(.plain)

            case .paused:
                Text(ticker.timeString)
                    .monospacedDigit()
                    .foregroundStyle(theme.secondary)
                Button(action: { pomodoro.resume() }) {
                    Image(systemName: "play")
                }
                .buttonStyle(.plain)
                Button(action: { pomodoro.stop() }) {
                    Image(systemName: "stop")
                }
                .buttonStyle(.plain)

            case .breakPending:
                Text("该休息了")
                    .foregroundStyle(theme.text)
                Button(action: { pomodoro.startBreak() }) {
                    Image(systemName: "cup.and.saucer")
                }
                .buttonStyle(.plain)
                .help("开始休息")
                Button(action: { pomodoro.skip() }) {
                    Image(systemName: "forward")
                }
                .buttonStyle(.plain)
                .help("跳过休息")

            case .breakTime:
                Label(ticker.timeString, systemImage: "cup.and.saucer")
                    .monospacedDigit()
                    .foregroundStyle(theme.secondary)
                Button(action: { pomodoro.skip() }) {
                    Image(systemName: "forward")
                }
                .buttonStyle(.plain)
            }
        }
        .font(.system(size: 11.5))
        .foregroundStyle(theme.secondary)
    }
}

/// Content of the focus-end prompt. Same ink-only palette as the panel; the primary
/// action is the one inverted block, the way the panel's active filter is.
struct BreakPromptView: View {
    let focusMinutes: Int
    let breakMinutes: Int
    let onStart: () -> Void
    let onSkip: () -> Void
    @Environment(\.colorScheme) private var colorScheme

    private var theme: Theme { Theme.of(colorScheme) }

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "cup.and.saucer")
                .font(.system(size: 24))
                .foregroundStyle(theme.text)
            VStack(spacing: 5) {
                Text("专注结束 · \(focusMinutes) 分钟")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(theme.text)
                Text("该休息 \(breakMinutes) 分钟了")
                    .font(.system(size: 12))
                    .foregroundStyle(theme.secondary)
            }
            HStack(spacing: 8) {
                promptButton("跳过", fill: theme.raised, ink: theme.text, action: onSkip)
                promptButton("开始休息", fill: theme.text, ink: theme.bg, action: onStart)
            }
        }
        .padding(.horizontal, 28)
        .padding(.top, 24)
        .padding(.bottom, 20)
        .frame(width: 300)
        .background(theme.bg)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(theme.line))
    }

    private func promptButton(_ title: String, fill: Color, ink: Color, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(ink)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 7)
                .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(fill))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// Owns the floating panel that shows `BreakPromptView`. The panel is non-activating,
/// so it appears over whatever the user is doing (other Spaces and full-screen apps
/// included) without taking keyboard focus — typing in progress can't dismiss it.
@MainActor
final class BreakPrompt {
    private var panel: NSPanel?

    func show(focusMinutes: Int, breakMinutes: Int, onStart: @escaping () -> Void, onSkip: @escaping () -> Void) {
        dismiss()
        let content = BreakPromptView(focusMinutes: focusMinutes, breakMinutes: breakMinutes,
                                      onStart: onStart, onSkip: onSkip)
        let hosting = FirstClickHostingView(rootView: content)
        let size = hosting.fittingSize
        let panel = NSPanel(contentRect: NSRect(origin: .zero, size: size),
                            styleMask: [.borderless, .nonactivatingPanel],
                            backing: .buffered, defer: false)
        panel.contentView = hosting
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.hidesOnDeactivate = false
        panel.isMovableByWindowBackground = true
        panel.isReleasedWhenClosed = false

        // Center on the screen the pointer is on — that's where the user is looking.
        let mouse = NSEvent.mouseLocation
        if let screen = NSScreen.screens.first(where: { NSMouseInRect(mouse, $0.frame, false) }) ?? NSScreen.main {
            let area = screen.visibleFrame
            panel.setFrameOrigin(NSPoint(x: area.midX - size.width / 2, y: area.midY - size.height / 2))
        }
        panel.orderFrontRegardless()
        self.panel = panel
    }

    func bringToFront() {
        panel?.orderFrontRegardless()
    }

    func dismiss() {
        panel?.orderOut(nil)
        panel = nil
    }
}

/// The prompt panel can never become key, so without this every click would be spent
/// trying to focus the window and the buttons would never fire.
private final class FirstClickHostingView<Content: View>: NSHostingView<Content> {
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
}

// MARK: - Self test

@MainActor
enum SelfTest {

    static func runAll() -> Bool {
        var allPass = true

        func check(_ name: String, _ condition: Bool, _ detail: String = "") {
            if condition {
                print("PASS: \(name)")
            } else {
                print("FAIL: \(name) — \(detail)")
                allPass = false
            }
        }

        // 1. Round-trip (new three-tier model)
        do {
            let data = TodoData(
                todayDate: "2026-07-17",
                todayItems: [TodoItem(text: "未完成任务", done: false), TodoItem(text: "已完成任务", done: true)],
                threeDayItems: [TodoItem(text: "三日任务", done: false, quadrant: .q2)],
                weekItems: [TodoItem(text: "本周任务", done: false)],
                archiveBody: "### 2026-07-16\n\n- [x] 昨天完成的任务"
            )
            let serialized = TodoParser.serialize(data)
            let parsed = TodoParser.parse(serialized)
            check("round-trip todayDate", parsed.todayDate == data.todayDate)
            check("round-trip todayItems", parsed.todayItems.map { [$0.text, "\($0.done)"] } == data.todayItems.map { [$0.text, "\($0.done)"] })
            check("round-trip threeDayItems", parsed.threeDayItems.map { [$0.text, "\($0.done)", "\(String(describing: $0.quadrant))"] } == data.threeDayItems.map { [$0.text, "\($0.done)", "\(String(describing: $0.quadrant))"] })
            check("round-trip weekItems", parsed.weekItems.map { [$0.text, "\($0.done)"] } == data.weekItems.map { [$0.text, "\($0.done)"] })
            check("round-trip archiveBody", parsed.archiveBody == data.archiveBody, "got: \(parsed.archiveBody)")
            check("serialize has bare 三日 header", serialized.contains("## 三日\n"))
            check("serialize has bare 本周 header", serialized.contains("## 本周\n"))
        }

        // 2. Legacy format migration: "## 本周 2026-W29" parses without data loss and
        //    is rewritten as bare "## 本周" on serialize.
        do {
            let legacy = """
            # Todos

            ## 今日 2026-07-17

            - [ ] 任务A !1
            - [x] 任务B

            ## 三日

            - [ ] 任务C !2

            ## 本周 2026-W29

            - [ ] 任务D

            ## 归档

            ### 2026-07-16

            - [x] 昨天完成的

            """
            let parsed = TodoParser.parse(legacy)
            check("legacy: todayDate parsed", parsed.todayDate == "2026-07-17")
            check("legacy: today count no data loss", parsed.todayItems.count == 2)
            check("legacy: today quadrant parsed", parsed.todayItems.first?.quadrant == .q1 && parsed.todayItems.first?.text == "任务A")
            check("legacy: threeDay count", parsed.threeDayItems.count == 1 && parsed.threeDayItems[0].quadrant == .q2)
            check("legacy: week items parsed from old-format header", parsed.weekItems.count == 1 && parsed.weekItems[0].text == "任务D")
            check("legacy: archive preserved", parsed.archiveBody.contains("昨天完成的"))

            let reserialized = TodoParser.serialize(parsed)
            check("legacy: rewritten with bare 本周 header", reserialized.contains("## 本周\n"))
            check("legacy: week id no longer present", !reserialized.contains("2026-W29"))
        }

        // 3. Quadrant marker parsing / serialization round-trip
        do {
            let (clean1, q1) = TodoParser.stripQuadrantSuffix("任务A !1")
            check("quadrant: trailing marker stripped", clean1 == "任务A" && q1 == .q1)

            let (clean2, q2) = TodoParser.stripQuadrantSuffix("任务!2 in middle of text")
            check("quadrant: mid-text marker not stripped", clean2 == "任务!2 in middle of text" && q2 == nil)

            let (clean3, q3) = TodoParser.stripQuadrantSuffix("没有标记的任务")
            check("quadrant: no marker leaves text untouched", clean3 == "没有标记的任务" && q3 == nil)

            let data = TodoData(
                todayDate: "2026-07-17",
                todayItems: [TodoItem(text: "紧急任务", done: false, quadrant: .q3)],
                threeDayItems: [],
                weekItems: [],
                archiveBody: ""
            )
            let serialized = TodoParser.serialize(data)
            check("quadrant: serialize appends suffix", serialized.contains("- [ ] 紧急任务 !3"))
            let reparsed = TodoParser.parse(serialized)
            check("quadrant: round-trip preserves quadrant", reparsed.todayItems.first?.quadrant == .q3)
            check("quadrant: round-trip preserves text", reparsed.todayItems.first?.text == "紧急任务")
        }

        // 3b. Relaxed quadrant marker (no space before !n) and focus-duration marker (⏱Nm),
        //     in either order, with mid-text occurrences left untouched.
        do {
            let (clean1, q1) = TodoParser.stripQuadrantSuffix("11需求!1")
            check("quadrant: unspaced marker parsed", clean1 == "11需求" && q1 == .q1, "got \(clean1) \(String(describing: q1))")

            let unspacedSerialized = TodoParser.serializeItem(TodoItem(text: "11需求", done: false, quadrant: .q1))
            check("quadrant: serialize always writes with a space", unspacedSerialized == "- [ ] 11需求 !1", "got \(unspacedSerialized)")

            let (cleanA, qA, mA) = TodoParser.stripSuffixMarkers("任务 !1 ⏱85m")
            check("duration: order '!1 ⏱85m' parses both markers",
                  cleanA == "任务" && qA == .q1 && mA == 85, "got \(cleanA) \(String(describing: qA)) \(String(describing: mA))")

            let (cleanB, qB, mB) = TodoParser.stripSuffixMarkers("任务 ⏱85m !1")
            check("duration: order '⏱85m !1' parses both markers",
                  cleanB == "任务" && qB == .q1 && mB == 85, "got \(cleanB) \(String(describing: qB)) \(String(describing: mB))")

            let durationOnly = TodoParser.stripSuffixMarkers("修 bug ⏱40m")
            check("duration: marker alone (no quadrant) parses", durationOnly.0 == "修 bug" && durationOnly.1 == nil && durationOnly.2 == 40)

            let midText = TodoParser.stripSuffixMarkers("任务 ⏱40m 备注 !2 结尾")
            check("duration/quadrant: mid-text occurrences not stripped",
                  midText.0 == "任务 ⏱40m 备注 !2 结尾" && midText.1 == nil && midText.2 == nil, "got \(midText.0)")

            let combinedSerialized = TodoParser.serializeItem(TodoItem(text: "任务", done: false, quadrant: .q1, focusMinutes: 85))
            check("duration+quadrant: serialize unified order '文本 !n ⏱Nm'", combinedSerialized == "- [ ] 任务 !1 ⏱85m", "got \(combinedSerialized)")

            let durationOnlySerialized = TodoParser.serializeItem(TodoItem(text: "修 bug", done: true, focusMinutes: 40))
            check("duration only: serialize omits quadrant suffix", durationOnlySerialized == "- [x] 修 bug ⏱40m", "got \(durationOnlySerialized)")

            // Full item-line parse round-trip through the real parser (both marker orders).
            let lineA = "- [ ] 写报告 !1 ⏱85m"
            let parsedLineA = TodoParser.parse("# Todos\n\n## 今日 2026-07-17\n\n\(lineA)\n")
            check("item line: '!1 ⏱85m' round-trips",
                  parsedLineA.todayItems.first?.text == "写报告" && parsedLineA.todayItems.first?.quadrant == .q1 && parsedLineA.todayItems.first?.focusMinutes == 85)

            let lineB = "- [ ] 写报告 ⏱85m !1"
            let parsedLineB = TodoParser.parse("# Todos\n\n## 今日 2026-07-17\n\n\(lineB)\n")
            check("item line: '⏱85m !1' round-trips",
                  parsedLineB.todayItems.first?.text == "写报告" && parsedLineB.todayItems.first?.quadrant == .q1 && parsedLineB.todayItems.first?.focusMinutes == 85)
        }

        // 3c. Focus-duration settlement: pure accumulate/credit functions.
        do {
            let items = [
                TodoItem(text: "已有时长", done: false, focusMinutes: 85),
                TodoItem(text: "无标记", done: false, focusMinutes: 0),
            ]
            let afterExisting = PomodoroSettlement.applyCredit(to: items, matchingText: "已有时长", minutesToAdd: 40)
            check("settlement: accumulates onto existing duration",
                  afterExisting.first(where: { $0.text == "已有时长" })?.focusMinutes == 125)

            let afterFresh = PomodoroSettlement.applyCredit(to: items, matchingText: "无标记", minutesToAdd: 12)
            check("settlement: sets duration on unmarked task",
                  afterFresh.first(where: { $0.text == "无标记" })?.focusMinutes == 12)

            let afterNoMatch = PomodoroSettlement.applyCredit(to: items, matchingText: "不存在的任务", minutesToAdd: 99)
            check("settlement: no match leaves items unchanged", afterNoMatch == items)

            check("settlement: natural completion credits full session length",
                  PomodoroSettlement.minutesToCredit(elapsedSeconds: 123, completedNaturally: true, sessionMinutes: 40) == 40)
            check("settlement: natural completion honors a non-default preset",
                  PomodoroSettlement.minutesToCredit(elapsedSeconds: 300, completedNaturally: true, sessionMinutes: 5) == 5)
            check("settlement: early stop floors to whole minutes",
                  PomodoroSettlement.minutesToCredit(elapsedSeconds: 125, completedNaturally: false, sessionMinutes: 40) == 2)
            check("settlement: sub-minute early stop discarded",
                  PomodoroSettlement.minutesToCredit(elapsedSeconds: 59, completedNaturally: false, sessionMinutes: 40) == 0)
        }

        // 3d. #tag parsing / extraction / canonical serialize order.
        do {
            /// Runs the real two-step pipeline (strip suffix markers, then extract
            /// tags from what remains) exactly as parseItemLine does.
            func parseTagLine(_ raw: String) -> (text: String, tags: [String], quadrant: Quadrant?, minutes: Int?) {
                let (afterSuffix, q, m) = TodoParser.stripSuffixMarkers(raw)
                let (clean, tags) = TodoParser.extractTags(afterSuffix)
                return (clean, tags, q, m)
            }

            // 1. Full round-trip through the real item-line parser: text/tags/quadrant/
            //    duration all correctly separated, and canonical serialize order is
            //    "text #tag1 #tag2 !n ⏱Nm".
            let line = "- [ ] 流畅优化 #maka-bazzi !1 ⏱45m"
            let parsedLine = TodoParser.parse("# Todos\n\n## 今日 2026-07-17\n\n\(line)\n")
            let item = parsedLine.todayItems.first
            check("tag: text extracted", item?.text == "流畅优化", "got \(String(describing: item?.text))")
            check("tag: tags extracted", item?.tags == ["maka-bazzi"], "got \(String(describing: item?.tags))")
            check("tag: quadrant still parsed", item?.quadrant == .q1)
            check("tag: duration still parsed", item?.focusMinutes == 45)
            if let item = item {
                let serialized = TodoParser.serializeItem(item)
                check("tag: canonical serialize order 'text #tag !n ⏱Nm'",
                      serialized == "- [ ] 流畅优化 #maka-bazzi !1 ⏱45m", "got \(serialized)")
            }

            // 2. A tag in the middle of the text is extracted and the display text is
            //    left clean (single-spaced, trimmed).
            let mid = parseTagLine("修 #a bug")
            check("tag: mid-text tag extracted, text cleaned",
                  mid.text == "修 bug" && mid.tags == ["a"], "got '\(mid.text)' \(mid.tags)")

            // 3. Multiple tags: first-seen order kept, duplicates collapsed; a lone
            //    '#' (nothing attached) is not a tag.
            let multi = parseTagLine("windows 适配 #maka-bazzi #紧急项目 #maka-bazzi")
            check("tag: dedup keeps first-seen order", multi.tags == ["maka-bazzi", "紧急项目"], "got \(multi.tags)")
            check("tag: text has tags stripped, whitespace collapsed", multi.text == "windows 适配", "got '\(multi.text)'")

            let lone = parseTagLine("裸 # 号 不算标签")
            check("tag: lone '#' is not a tag", lone.tags.isEmpty && lone.text == "裸 # 号 不算标签", "got '\(lone.text)' \(lone.tags)")

            // 4. Tag combined with quadrant/duration in varying orders all parse and
            //    re-serialize to the same unified canonical order. (Per the parse
            //    pipeline — suffix-strip first from the line's absolute end, then
            //    extract tags from what remains — a tag must precede the trailing
            //    quadrant/duration cluster to be recognized together with them; the
            //    quadrant/duration pair itself may appear in either relative order,
            //    same as before tags existed.)
            let variants: [(String, String)] = [
                ("任务 #x !2 ⏱10m", "tag, quadrant, duration"),
                ("任务 #x ⏱10m !2", "tag, duration, quadrant"),
                ("#x 任务 !2 ⏱10m", "tag leading"),
            ]
            for (variant, label) in variants {
                let parsed = parseTagLine(variant)
                check("tag/order (\(label)): parses text+tag+quadrant+duration",
                      parsed.text == "任务" && parsed.tags == ["x"] && parsed.quadrant == .q2 && parsed.minutes == 10,
                      "got text='\(parsed.text)' tags=\(parsed.tags) q=\(String(describing: parsed.quadrant)) m=\(String(describing: parsed.minutes))")
                let reserialized = TodoParser.serializeItem(TodoItem(text: parsed.text, done: false, quadrant: parsed.quadrant, focusMinutes: parsed.minutes ?? 0, tags: parsed.tags))
                check("tag/order (\(label)): re-serializes to canonical '任务 #x !2 ⏱10m'",
                      reserialized == "- [ ] 任务 #x !2 ⏱10m", "got \(reserialized)")
            }

            // Tag with only a quadrant marker, and tag with only a duration marker.
            let quadOnly = parseTagLine("任务 #y !3")
            check("tag+quadrant only: parses", quadOnly.text == "任务" && quadOnly.tags == ["y"] && quadOnly.quadrant == .q3 && quadOnly.minutes == nil)
            let quadOnlySerialized = TodoParser.serializeItem(TodoItem(text: quadOnly.text, done: false, quadrant: quadOnly.quadrant, tags: quadOnly.tags))
            check("tag+quadrant only: serializes to 'text #tag !n'", quadOnlySerialized == "- [ ] 任务 #y !3", "got \(quadOnlySerialized)")

            let durationOnly = parseTagLine("任务 #z ⏱20m")
            check("tag+duration only: parses", durationOnly.text == "任务" && durationOnly.tags == ["z"] && durationOnly.quadrant == nil && durationOnly.minutes == 20)
            let durationOnlySerialized = TodoParser.serializeItem(TodoItem(text: durationOnly.text, done: false, focusMinutes: durationOnly.minutes ?? 0, tags: durationOnly.tags))
            check("tag+duration only: serializes to 'text #tag ⏱Nm'", durationOnlySerialized == "- [ ] 任务 #z ⏱20m", "got \(durationOnlySerialized)")

            // No-tags item still serializes exactly as before (no stray space).
            let noTagSerialized = TodoParser.serializeItem(TodoItem(text: "普通任务", done: false, quadrant: .q1))
            check("no tags: serialize unchanged", noTagSerialized == "- [ ] 普通任务 !1", "got \(noTagSerialized)")

        }

        // 4. New rollover: completed items across all three tiers consolidate into one
        //    archive sub-section; unfinished items stay; old archive content preserved.
        do {
            let input = TodoData(
                todayDate: "2020-01-01",
                todayItems: [TodoItem(text: "today done", done: true), TodoItem(text: "today undone", done: false)],
                threeDayItems: [TodoItem(text: "3day done", done: true), TodoItem(text: "3day undone", done: false)],
                weekItems: [TodoItem(text: "week done", done: true), TodoItem(text: "week undone", done: false)],
                archiveBody: "### 2019-12-31\n\n- [x] old archived item"
            )
            let (rolled, changed) = Rollover.apply(input)
            check("rollover: changed flag", changed)
            check("rollover: todayDate updated", rolled.todayDate == DateUtil.todayDateString())
            check("rollover: today undone kept", rolled.todayItems.map { $0.text } == ["today undone"])
            check("rollover: threeDay undone kept", rolled.threeDayItems.map { $0.text } == ["3day undone"])
            check("rollover: week undone kept", rolled.weekItems.map { $0.text } == ["week undone"])
            check("rollover: archive block created for old date", rolled.archiveBody.contains("### 2020-01-01"))
            check("rollover: all done items archived together",
                  rolled.archiveBody.contains("today done") && rolled.archiveBody.contains("3day done") && rolled.archiveBody.contains("week done"))
            check("rollover: old archive content preserved", rolled.archiveBody.contains("2019-12-31") && rolled.archiveBody.contains("old archived item"))
            if let newRange = rolled.archiveBody.range(of: "2020-01-01"), let oldRange = rolled.archiveBody.range(of: "2019-12-31") {
                check("rollover: newest archive on top", newRange.lowerBound < oldRange.lowerBound)
            } else {
                check("rollover: newest archive on top", false, "ranges not found")
            }
            let occurrences = rolled.archiveBody.components(separatedBy: "### 2020-01-01").count - 1
            check("rollover: single combined block, not one per tier", occurrences == 1, "got \(occurrences) occurrences")
        }

        // 5. Section sort: quadrant priority (1<2<3<4<unclassified), file order within
        //    same quadrant, completed items always last.
        do {
            let items = [
                TodoItem(text: "unclassified1", done: false, quadrant: nil),
                TodoItem(text: "q3item", done: false, quadrant: .q3),
                TodoItem(text: "q1item", done: false, quadrant: .q1),
                TodoItem(text: "doneq1", done: true, quadrant: .q1),
                TodoItem(text: "q2item", done: false, quadrant: .q2),
                TodoItem(text: "unclassified2", done: false, quadrant: nil),
            ]
            let sorted = TodoSort.sorted(items)
            let order = sorted.map { $0.text }
            check("sort: quadrant priority + done last",
                  order == ["q1item", "q2item", "q3item", "unclassified1", "unclassified2", "doneq1"],
                  "got \(order)")
        }

        // 6. Missing sections / empty file tolerance
        do {
            let empty = TodoParser.parse("")
            check("empty file: todayDate empty", empty.todayDate.isEmpty)
            check("empty file: no crash / empty items", empty.todayItems.isEmpty && empty.threeDayItems.isEmpty && empty.weekItems.isEmpty && empty.archiveBody.isEmpty)

            let onlyToday = "# Todos\n\n## 今日 2026-07-17\n\n- [ ] a\n- [x] b\n"
            let parsedOnlyToday = TodoParser.parse(onlyToday)
            check("missing threeDay/week/archive: today parsed", parsedOnlyToday.todayItems.count == 2)
            check("missing threeDay/week/archive: threeDay empty", parsedOnlyToday.threeDayItems.isEmpty)
            check("missing threeDay/week/archive: week empty", parsedOnlyToday.weekItems.isEmpty)
            check("missing threeDay/week/archive: archive empty", parsedOnlyToday.archiveBody.isEmpty)

            // Rollover on missing today section: no weird empty-title archive block.
            let (rolledMissing, changedMissing) = Rollover.apply(TodoData.empty(todayDate: ""))
            check("rollover on missing sections changes flag", changedMissing)
            check("rollover on missing sections sets today date", rolledMissing.todayDate == DateUtil.todayDateString())
            check("rollover on missing sections: no archive block created", rolledMissing.archiveBody.isEmpty, "got: \(rolledMissing.archiveBody)")

            // Garbage/unrecognized lines inside today/threeDay/week are dropped without crashing.
            let garbage = "# Todos\n\n## 今日 2026-07-17\n\nrandom text\n- [ ] real item\n\n## 三日\n\nmore junk\n\n## 本周\n\nmore junk2\n\n## 归档\n\nkeep me raw\n### sub\n"
            let parsedGarbage = TodoParser.parse(garbage)
            check("garbage lines dropped in today", parsedGarbage.todayItems.count == 1 && parsedGarbage.todayItems[0].text == "real item")
            check("garbage lines dropped in threeDay", parsedGarbage.threeDayItems.isEmpty)
            check("garbage lines dropped in week", parsedGarbage.weekItems.isEmpty)
            check("archive preserved raw", parsedGarbage.archiveBody.contains("keep me raw") && parsedGarbage.archiveBody.contains("### sub"))
        }

        // 7. Uses temp directory only — verify file-based load/save/rollover round trip end-to-end.
        do {
            let tmpDir = FileManager.default.temporaryDirectory.appendingPathComponent("hibi-selftest-\(UUID().uuidString)")
            try? FileManager.default.createDirectory(at: tmpDir, withIntermediateDirectories: true)
            let tmpFile = tmpDir.appendingPathComponent("todos.md")

            // Nonexistent file -> created fresh.
            let fresh = TodoFile.loadWithRollover(from: tmpFile)
            check("temp file created on first load", FileManager.default.fileExists(atPath: tmpFile.path))
            check("temp file fresh has current date", fresh.todayDate == DateUtil.todayDateString())
            check("temp file fresh has no archive block", fresh.archiveBody.isEmpty)

            // Write stale content directly, then reload triggers rollover + persists.
            let stale = TodoData(
                todayDate: "2000-01-01",
                todayItems: [TodoItem(text: "stale done", done: true), TodoItem(text: "stale undone", done: false)],
                threeDayItems: [TodoItem(text: "stale 3day done", done: true)],
                weekItems: [],
                archiveBody: ""
            )
            try? TodoFile.save(stale, to: tmpFile)
            let reloaded = TodoFile.loadWithRollover(from: tmpFile)
            check("temp file rollover updates date", reloaded.todayDate == DateUtil.todayDateString())
            check("temp file rollover keeps undone", reloaded.todayItems.map { $0.text } == ["stale undone"])
            check("temp file rollover archives done from all tiers", reloaded.archiveBody.contains("stale done") && reloaded.archiveBody.contains("stale 3day done"))

            // Verify it was actually persisted to disk (re-read from disk without going through rollover again).
            let onDisk = TodoFile.load(from: tmpFile)
            check("temp file persisted after rollover", onDisk.todayDate == DateUtil.todayDateString())

            // Also verify a legacy-format file on disk migrates cleanly through the real load path.
            let legacyFile = tmpDir.appendingPathComponent("legacy.md")
            let legacyText = "# Todos\n\n## 今日 \(DateUtil.todayDateString())\n\n- [ ] a\n\n## 本周 2099-W01\n\n- [ ] b\n\n## 归档\n\n"
            try? legacyText.write(to: legacyFile, atomically: true, encoding: .utf8)
            let legacyLoaded = TodoFile.loadWithRollover(from: legacyFile)
            check("legacy file on disk: no data loss", legacyLoaded.todayItems.count == 1 && legacyLoaded.weekItems.count == 1)
            let legacyOnDisk = try? String(contentsOf: legacyFile, encoding: .utf8)
            check("legacy file on disk: rewritten to bare 本周 if changed, else left parseable", legacyOnDisk?.contains("b") ?? false)

            try? FileManager.default.removeItem(at: tmpDir)
        }

        // 8. PanelLayout: the list area always resolves to a definite, non-zero
        //    height — a 0 here is exactly the MenuBarExtra collapse this guards against.
        do {
            check("layout: unmeasured content falls back to the floor",
                  PanelLayout.listHeight(contentHeight: 0) == PanelLayout.minListHeight)
            check("layout: negative/degenerate content falls back to the floor",
                  PanelLayout.listHeight(contentHeight: -12) == PanelLayout.minListHeight)
            check("layout: short content is raised to the floor",
                  PanelLayout.listHeight(contentHeight: 20) == PanelLayout.minListHeight)
            check("layout: ordinary content is used verbatim",
                  PanelLayout.listHeight(contentHeight: 212) == 212)
            check("layout: tall content is capped so the list scrolls",
                  PanelLayout.listHeight(contentHeight: 900) == PanelLayout.maxListHeight)
            check("layout: never returns zero",
                  [CGFloat(0), -1, 1, 67, 68, 359, 360, 361, 10_000]
                      .allSatisfy { PanelLayout.listHeight(contentHeight: $0) > 0 })
        }

        // 9. In-place editing: the raw spelling the editor hands the user, what an edit
        //    preserves, and the id-blind content comparison that keeps an edit alive
        //    across the file-watcher echo of its own save.
        do {
            let today = DateUtil.todayDateString()

            // sameContent must ignore the per-parse id — otherwise every persist() would
            // look like an external change, and the reload would mint new ids and drop
            // the row being edited out from under the editor.
            let a = TodoData(todayDate: today, todayItems: [TodoItem(text: "x", done: false)],
                             threeDayItems: [], weekItems: [], archiveBody: "")
            let b = TodoData(todayDate: today, todayItems: [TodoItem(text: "x", done: false)],
                             threeDayItems: [], weekItems: [], archiveBody: "")
            check("edit: sameContent ignores freshly-minted ids", a.sameContent(as: b) && a != b)
            var changed = b
            changed.todayItems[0].done = true
            check("edit: sameContent still catches a real change", !a.sameContent(as: changed))
            var reordered = b
            reordered.todayItems.append(TodoItem(text: "y", done: false))
            check("edit: sameContent catches an added item", !a.sameContent(as: reordered))

            let tmpDir = FileManager.default.temporaryDirectory
                .appendingPathComponent("hibi-selftest-edit-\(UUID().uuidString)")
            try? FileManager.default.createDirectory(at: tmpDir, withIntermediateDirectories: true)
            let file = tmpDir.appendingPathComponent("todos.md")
            let fixture = """
            # Todos

            ## 今日 \(today)

            - [ ] 写周报 #work !2 ⏱40m

            """
            try? fixture.write(to: file, atomically: true, encoding: .utf8)

            let state = AppState(fileURL: file)
            if let item = state.data.todayItems.first {
                // What the editor prefills: exactly what quick-add would have accepted,
                // minus ⏱ — the timer owns that, not the keyboard.
                let editable = item.editableText
                check("edit: editable text is the quick-add spelling", editable == "写周报 #work !2", "got '\(editable)'")

                let before = state.data
                check("edit: feeding the editable text straight back is a no-op",
                      state.updateText(item.id, raw: editable, in: .today) && state.data.sameContent(as: before))

                check("edit: a real rewrite is accepted",
                      state.updateText(item.id, raw: "写月报 #ops !1", in: .today))
                if let after = state.data.todayItems.first {
                    check("edit: text, tags and quadrant are replaced",
                          after.text == "写月报" && after.tags == ["ops"] && after.quadrant == .q1,
                          "got text='\(after.text)' tags=\(after.tags) q=\(String(describing: after.quadrant))")
                    check("edit: focus minutes survive an edit that never mentions them",
                          after.focusMinutes == 40, "got \(after.focusMinutes)")
                    check("edit: the row keeps its id, so the editor survives the save",
                          after.id == item.id)
                }

                // An explicit ⏱ marker is still honored — it is the one way to correct a
                // miscounted total by hand.
                check("edit: an explicit duration marker overrides the running total",
                      state.updateText(item.id, raw: "写月报 #ops !1 ⏱5m", in: .today)
                          && state.data.todayItems.first?.focusMinutes == 5)

                // Degenerate input is refused rather than blanking the task.
                check("edit: whitespace-only input is refused", !state.updateText(item.id, raw: "   ", in: .today))
                check("edit: tag/marker-only input is refused", !state.updateText(item.id, raw: "#ops !1", in: .today))
                check("edit: a refused edit leaves the task untouched",
                      state.data.todayItems.first?.text == "写月报")

                let onDisk = (try? String(contentsOf: file, encoding: .utf8)) ?? ""
                check("edit: the edit reached disk in canonical order",
                      onDisk.contains("- [ ] 写月报 #ops !1 ⏱5m"), "got \(onDisk)")
            } else {
                check("edit: fixture loaded", false, "today section parsed empty")
            }

            try? FileManager.default.removeItem(at: tmpDir)
        }

        print(allPass ? "ALL PASS" : "SOME FAILED")
        return allPass
    }
}

// MARK: - App entry point

@main
struct HibiApp: App {
    @StateObject private var appState: AppState
    @StateObject private var pomodoro: PomodoroState

    init() {
        NSApplication.shared.setActivationPolicy(.accessory)
        if CommandLine.arguments.contains("--selftest") {
            let ok = SelfTest.runAll()
            exit(ok ? 0 : 1)
        }
        let state = AppState()
        let pomo = PomodoroState()
        pomo.onSettle = { [weak state] text, tier, minutes in
            state?.creditFocusMinutes(text: text, tier: tier, minutesToAdd: minutes)
        }
        _appState = StateObject(wrappedValue: state)
        _pomodoro = StateObject(wrappedValue: pomo)
    }

    var body: some Scene {
        MenuBarExtra {
            PanelView(appState: appState, pomodoro: pomodoro)
        } label: {
            MenuBarLabel(appState: appState, pomodoro: pomodoro, ticker: pomodoro.ticker)
        }
        .menuBarExtraStyle(.window)
    }
}

/// The countdown lives here rather than inline in `HibiApp.body` so the ticking second
/// only invalidates this view, not the whole scene — a scene rebuild would recreate
/// `PanelView` and every row's context menu with it (see `PomodoroTicker`).
struct MenuBarLabel: View {
    @ObservedObject var appState: AppState
    @ObservedObject var pomodoro: PomodoroState
    @ObservedObject var ticker: PomodoroTicker

    var body: some View {
        switch pomodoro.phase {
        case .focus, .paused:
            Text(ticker.timeString)
                .monospacedDigit()
        case .breakPending:
            Label("该休息了", systemImage: "cup.and.saucer.fill")
        case .breakTime:
            Label(ticker.timeString, systemImage: "cup.and.saucer.fill")
                .monospacedDigit()
        case .idle:
            if appState.todayRemainingCount > 0 {
                Label {
                    Text("\(appState.todayRemainingCount)")
                } icon: {
                    Image(nsImage: CatGlyph.menuBar)
                }
            } else {
                Image(nsImage: CatGlyph.menuBar)
            }
        }
    }
}
