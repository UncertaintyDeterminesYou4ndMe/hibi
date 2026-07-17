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

// MARK: - Tag color palette

enum TagPalette {
    /// 8 soft (background, foreground) color pairs for tag chips.
    static let palette: [(Color, Color)] = [
        (Color(red: 0.86, green: 0.91, blue: 0.98), Color(red: 0.13, green: 0.35, blue: 0.65)), // blue
        (Color(red: 0.87, green: 0.95, blue: 0.87), Color(red: 0.16, green: 0.45, blue: 0.24)), // green
        (Color(red: 0.99, green: 0.90, blue: 0.78), Color(red: 0.62, green: 0.36, blue: 0.05)), // orange
        (Color(red: 0.98, green: 0.85, blue: 0.87), Color(red: 0.66, green: 0.16, blue: 0.27)), // pink
        (Color(red: 0.92, green: 0.87, blue: 0.98), Color(red: 0.44, green: 0.24, blue: 0.66)), // purple
        (Color(red: 0.99, green: 0.95, blue: 0.78), Color(red: 0.55, green: 0.46, blue: 0.04)), // yellow
        (Color(red: 0.84, green: 0.95, blue: 0.95), Color(red: 0.09, green: 0.44, blue: 0.44)), // teal
        (Color(red: 0.91, green: 0.91, blue: 0.92), Color(red: 0.36, green: 0.36, blue: 0.38)), // gray
    ]

    /// Deterministic djb2-style hash over the tag name. Swift's built-in String
    /// hashing is randomized per process launch (DoS hardening), which would make a
    /// tag's chip color change across app restarts — this hash is stable instead, so
    /// the same tag name always maps to the same palette entry.
    static func stableIndex(for tag: String) -> Int {
        var hash: UInt64 = 5381
        for scalar in tag.unicodeScalars {
            hash = ((hash << 5) &+ hash) &+ UInt64(scalar.value)
        }
        return Int(hash % UInt64(palette.count))
    }

    static func colorPair(for tag: String) -> (background: Color, foreground: Color) {
        let (bg, fg) = palette[stableIndex(for: tag)]
        return (bg, fg)
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

    func reloadFromDisk() {
        data = TodoFile.loadWithRollover(from: fileURL)
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
    case breakTime
}

/// Pure functions for computing and applying focus-duration credit. Kept free of any
/// timer/UI state so they can be exercised directly in self-tests.
@MainActor
enum PomodoroSettlement {
    /// Minutes to credit for a focus session ending either by running out naturally
    /// (full `PomodoroState.focusMinutes`) or by being stopped early (the whole minutes
    /// actually ticked down, floored; a partial minute is discarded).
    static func minutesToCredit(elapsedSeconds: Int, completedNaturally: Bool) -> Int {
        if completedNaturally {
            return PomodoroState.focusMinutes
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

@MainActor
final class PomodoroState: ObservableObject {
    static let focusMinutes = 40
    static let breakMinutes = 10

    @Published private(set) var phase: PomodoroPhase = .idle
    @Published private(set) var remainingSeconds: Int = 0

    /// Task this focus session is bound to (nil = free/unbound focus, v2 behavior: no
    /// duration is tracked). Cleared whenever a session ends (naturally or via stop).
    @Published private(set) var boundTaskId: UUID? = nil
    @Published private(set) var boundTaskTier: Tier? = nil
    @Published private(set) var boundTaskText: String? = nil

    /// Invoked to persist settled focus minutes onto the bound task: (text, tier, minutes).
    var onSettle: ((String, Tier, Int) -> Void)?

    private var timer: Timer?
    private var notificationsAuthorized = false

    init() {
        requestAuthorization()
    }

    private func requestAuthorization() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { [weak self] granted, _ in
            Task { @MainActor in
                self?.notificationsAuthorized = granted
            }
        }
    }

    var timeString: String {
        let m = max(0, remainingSeconds) / 60
        let s = max(0, remainingSeconds) % 60
        return String(format: "%02d:%02d", m, s)
    }

    /// Starts a free focus session, not bound to any task (v2 behavior — no duration
    /// tracked). If a session is already running for another (bound or free) task, it
    /// is settled first.
    func start() {
        beginFocus(taskId: nil, tier: nil, text: nil)
    }

    /// Starts a focus session bound to a specific task. If a session is already
    /// running, it is settled first (see PomodoroSettlement).
    func startFocus(taskId: UUID, tier: Tier, text: String) {
        beginFocus(taskId: taskId, tier: tier, text: text)
    }

    private func beginFocus(taskId: UUID?, tier: Tier?, text: String?) {
        if phase == .focus || phase == .paused {
            settle(naturalCompletion: false)
        }
        boundTaskId = taskId
        boundTaskTier = tier
        boundTaskText = text
        phase = .focus
        remainingSeconds = Self.focusMinutes * 60
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

    func skip() {
        guard phase == .breakTime else { return }
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
        let elapsed = Self.focusMinutes * 60 - remainingSeconds
        let minutes = PomodoroSettlement.minutesToCredit(elapsedSeconds: elapsed, completedNaturally: naturalCompletion)
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
                notifyAndChime(title: "专注结束", body: "休息 10 分钟")
                phase = .breakTime
                remainingSeconds = Self.breakMinutes * 60
            case .breakTime:
                notifyAndChime(title: "休息结束", body: "开始下一轮吧")
                stop()
            default:
                break
            }
        }
    }

    private func notifyAndChime(title: String, body: String) {
        if notificationsAuthorized {
            let content = UNMutableNotificationContent()
            content.title = title
            content.body = body
            let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
            UNUserNotificationCenter.current().add(request)
        }
        NSSound(named: "Glass")?.play()
    }
}

// MARK: - UI

struct PanelView: View {
    @ObservedObject var appState: AppState
    @ObservedObject var pomodoro: PomodoroState
    @State private var quickAddText: String = ""
    @FocusState private var quickAddFocused: Bool
    @State private var eventMonitor: Any?
    @State private var hoveringId: UUID?
    @State private var showHelp: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            Divider()
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if appState.activeTagFilter != nil {
                        filteredSectionsView
                    } else {
                        todaySection
                        if !appState.data.threeDayItems.isEmpty {
                            Divider()
                            threeDaySection
                        }
                        if !appState.data.weekItems.isEmpty {
                            Divider()
                            weekSection
                        }
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
            }
            .frame(maxHeight: 360)
            Divider()
            quickAddBar
            Divider()
            bottomToolbar
        }
        .frame(width: 320)
        .fontDesign(.rounded)
        .background(.ultraThinMaterial)
        .onAppear {
            appState.reloadFromDisk()
            installEventMonitor()
        }
        .onDisappear {
            removeEventMonitor()
        }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            appState.reloadFromDisk()
        }
    }

    // MARK: Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("今日")
                        .font(.system(size: 17, weight: .semibold))
                    Text(DateUtil.headerDisplay(for: appState.data.todayDate))
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if appState.activeTagFilter != nil {
                    ProgressRing(completed: appState.filteredTodayCompletedCount, total: appState.filteredTodayTotalCount)
                } else {
                    ProgressRing(completed: appState.todayCompletedCount, total: appState.todayTotalCount)
                }
            }
            if let tag = appState.activeTagFilter {
                filterBar(tag)
            }
            HStack {
                Spacer()
                PomodoroControl(pomodoro: pomodoro)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }

    private func filterBar(_ tag: String) -> some View {
        let colors = TagPalette.colorPair(for: tag)
        return HStack(spacing: 4) {
            Text("#\(tag)")
                .font(.system(size: 11, weight: .medium))
            Button(action: { appState.setTagFilter(nil) }) {
                Image(systemName: "xmark")
                    .font(.system(size: 9, weight: .semibold))
            }
            .buttonStyle(.plain)
        }
        .foregroundStyle(colors.foreground)
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(Capsule().fill(colors.background))
    }

    // MARK: Sections

    private var todaySection: some View {
        Group {
            if appState.data.todayItems.isEmpty {
                Text("今天没有待办 🎉")
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.vertical, 16)
            } else {
                sectionView(tier: .today, items: appState.data.todayItems, title: nil,
                            moveTargets: [(.threeDay, "三日"), (.week, "本周")])
            }
        }
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
                Text("没有匹配的任务")
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.vertical, 16)
            } else {
                ForEach(Array(visible.enumerated()), id: \.offset) { idx, entry in
                    if idx > 0 { Divider() }
                    sectionView(tier: entry.0, items: entry.3, title: entry.1, moveTargets: entry.2)
                }
            }
        }
    }

    private func sectionView(tier: Tier, items: [TodoItem], title: String?, moveTargets: [(Tier, String)]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            if let title = title {
                Text(title)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(.secondary)
                    .padding(.bottom, 2)
            }
            ForEach(TodoSort.sorted(items)) { item in
                TodoRow(
                    item: item,
                    isHovering: hoveringId == item.id,
                    isFocusing: pomodoro.phase == .focus && pomodoro.boundTaskId == item.id,
                    onToggle: { appState.toggle(item.id, in: tier) },
                    onDelete: { appState.delete(item.id, in: tier) },
                    onStartFocus: { pomodoro.startFocus(taskId: item.id, tier: tier, text: item.text) },
                    onHover: { hovering in hoveringId = hovering ? item.id : nil },
                    onTagTap: { tag in appState.toggleTagFilter(tag) }
                )
                .contextMenu {
                    Button("开始专注") { pomodoro.startFocus(taskId: item.id, tier: tier, text: item.text) }
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
        TextField(quickAddPlaceholder, text: $quickAddText)
            .textFieldStyle(.plain)
            .font(.system(size: 13))
            .focused($quickAddFocused)
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .onSubmit {
                submitQuickAdd(to: .today)
            }
    }

    private func installEventMonitor() {
        guard eventMonitor == nil else { return }
        eventMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
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

    private var bottomToolbar: some View {
        HStack {
            Button("打开文件") {
                NSWorkspace.shared.open(appState.fileURL)
            }
            .buttonStyle(.plain)
            .font(.system(size: 12))
            .foregroundStyle(.secondary)

            Button(action: { showHelp.toggle() }) {
                Image(systemName: "questionmark.circle")
            }
            .buttonStyle(.plain)
            .font(.system(size: 12))
            .foregroundStyle(.secondary)
            .popover(isPresented: $showHelp, arrowEdge: .top) {
                helpContent
            }

            Spacer()

            Button("退出") {
                NSApp.terminate(nil)
            }
            .buttonStyle(.plain)
            .font(.system(size: 12))
            .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
    }

    // MARK: Help popover

    private var helpContent: some View {
        VStack(alignment: .leading, spacing: 8) {
            helpSection(title: "添加", lines: [
                "↩ 加到今日 · ⌥↩ 三日 · ⌘↩ 本周",
                "任务结尾写 !1~!4 标象限",
                "#项目名 打标签，点击标签可筛选",
            ])
            helpSection(title: "象限", lines: [
                "!1 重要紧急(红) !2 重要不紧急(蓝) !3 紧急不重要(橙) !4 都不(灰)",
                "右键任务可改象限/移动层级",
            ])
            helpSection(title: "专注", lines: [
                "悬停任务点 ▶ 为它计时 · 40 分钟专注 + 10 分钟休息 · 时长自动记到任务上",
            ])
        }
        .padding(10)
        .frame(width: 260, alignment: .leading)
    }

    private func helpSection(title: String, lines: [String]) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(.system(size: 12, weight: .semibold))
            ForEach(lines, id: \.self) { line in
                Text(line)
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct TodoRow: View {
    let item: TodoItem
    let isHovering: Bool
    let isFocusing: Bool
    let onToggle: () -> Void
    let onDelete: () -> Void
    let onStartFocus: () -> Void
    let onHover: (Bool) -> Void
    let onTagTap: (String) -> Void

    private var circleColor: Color {
        guard !item.done else { return .green }
        switch item.quadrant {
        case .q1: return Color(nsColor: .systemRed)
        case .q2: return Color(nsColor: .systemBlue)
        case .q3: return Color(nsColor: .systemOrange)
        case .q4: return .gray
        case .none: return .secondary
        }
    }

    private func formattedFocusDuration(_ minutes: Int) -> String {
        guard minutes >= 60 else { return "⏱ \(minutes)m" }
        let h = minutes / 60
        let m = minutes % 60
        return m > 0 ? "⏱ \(h)h\(m)m" : "⏱ \(h)h"
    }

    var body: some View {
        HStack(spacing: 8) {
            Button(action: onToggle) {
                Image(systemName: item.done ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(circleColor)
            }
            .buttonStyle(.plain)

            Text(item.text)
                .font(.system(size: 13))
                .lineSpacing(2)
                .strikethrough(item.done)
                .foregroundStyle(item.done ? .secondary : .primary)
                .lineLimit(2)

            ForEach(item.tags, id: \.self) { tag in
                TagChip(tag: tag, dimmed: item.done, onTap: { onTagTap(tag) })
            }

            if item.focusMinutes > 0 {
                Text(formattedFocusDuration(item.focusMinutes))
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }

            Spacer()

            if isFocusing {
                Image(systemName: "timer")
                    .foregroundStyle(.secondary)
                    .font(.system(size: 11))
            } else if isHovering {
                Button(action: onStartFocus) {
                    Image(systemName: "play.circle")
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }

            if isHovering {
                Button(action: onDelete) {
                    Image(systemName: "xmark")
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
        .onHover { hovering in
            onHover(hovering)
        }
    }
}

/// A small clickable pill showing a tag name (without the leading `#`). Background
/// color is chosen deterministically from an 8-color palette by the tag name's hash,
/// so the same tag always renders in the same color. Grays out when `dimmed` (used
/// for tags on completed items).
struct TagChip: View {
    let tag: String
    let dimmed: Bool
    let onTap: () -> Void

    var body: some View {
        let colors = TagPalette.colorPair(for: tag)
        Button(action: onTap) {
            Text(tag)
                .font(.system(size: 10))
                .foregroundStyle(dimmed ? Color.secondary : colors.foreground)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Capsule().fill(dimmed ? Color.secondary.opacity(0.15) : colors.background))
        }
        .buttonStyle(.plain)
    }
}

struct ProgressRing: View {
    let completed: Int
    let total: Int

    private var fraction: CGFloat {
        guard total > 0 else { return 0 }
        return CGFloat(completed) / CGFloat(total)
    }

    private var isComplete: Bool {
        total > 0 && completed == total
    }

    var body: some View {
        HStack(spacing: 6) {
            ZStack {
                Circle()
                    .stroke(Color.secondary.opacity(0.25), lineWidth: 3)
                Circle()
                    .trim(from: 0, to: fraction)
                    .stroke(isComplete ? Color.green : Color.accentColor, style: StrokeStyle(lineWidth: 3, lineCap: .round))
                    .rotationEffect(.degrees(-90))
            }
            .frame(width: 18, height: 18)

            Text("\(completed)/\(total)")
                .font(.system(size: 12))
                .monospacedDigit()
                .foregroundStyle(.secondary)
        }
    }
}

struct PomodoroControl: View {
    @ObservedObject var pomodoro: PomodoroState

    /// Truncates a bound task's name to roughly 12 characters for the header display.
    private func truncatedTaskName(_ text: String) -> String {
        let maxLen = 12
        guard text.count > maxLen else { return text }
        return String(text.prefix(maxLen)) + "…"
    }

    var body: some View {
        HStack(spacing: 6) {
            switch pomodoro.phase {
            case .idle:
                Button(action: { pomodoro.start() }) {
                    Label("开始专注", systemImage: "play.fill")
                }
                .buttonStyle(.plain)

            case .focus:
                if let taskText = pomodoro.boundTaskText {
                    Text(truncatedTaskName(taskText))
                        .lineLimit(1)
                }
                Text(pomodoro.timeString)
                    .monospacedDigit()
                Button(action: { pomodoro.pause() }) {
                    Image(systemName: "pause.fill")
                }
                .buttonStyle(.plain)
                Button(action: { pomodoro.stop() }) {
                    Image(systemName: "stop.fill")
                }
                .buttonStyle(.plain)

            case .paused:
                Text(pomodoro.timeString)
                    .monospacedDigit()
                Button(action: { pomodoro.resume() }) {
                    Image(systemName: "play.fill")
                }
                .buttonStyle(.plain)
                Button(action: { pomodoro.stop() }) {
                    Image(systemName: "stop.fill")
                }
                .buttonStyle(.plain)

            case .breakTime:
                Label("休息 \(pomodoro.timeString)", systemImage: "cup.and.saucer.fill")
                    .monospacedDigit()
                Button(action: { pomodoro.skip() }) {
                    Image(systemName: "forward.fill")
                }
                .buttonStyle(.plain)
            }
        }
        .font(.system(size: 12))
        .foregroundStyle(.secondary)
    }
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

            check("settlement: natural completion credits full focus length",
                  PomodoroSettlement.minutesToCredit(elapsedSeconds: 123, completedNaturally: true) == PomodoroState.focusMinutes)
            check("settlement: early stop floors to whole minutes",
                  PomodoroSettlement.minutesToCredit(elapsedSeconds: 125, completedNaturally: false) == 2)
            check("settlement: sub-minute early stop discarded",
                  PomodoroSettlement.minutesToCredit(elapsedSeconds: 59, completedNaturally: false) == 0)
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

            // TagPalette color assignment is stable (deterministic hash, not Swift's
            // randomized-per-process String hashing) — same tag name always maps to
            // the same palette index.
            let idxA = TagPalette.stableIndex(for: "maka-bazzi")
            let idxB = TagPalette.stableIndex(for: "maka-bazzi")
            check("tag color: stable index is deterministic for same name", idxA == idxB && idxA >= 0 && idxA < TagPalette.palette.count)
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
            menuBarLabel
        }
        .menuBarExtraStyle(.window)
    }

    @ViewBuilder
    private var menuBarLabel: some View {
        switch pomodoro.phase {
        case .focus, .paused:
            Text(pomodoro.timeString)
                .monospacedDigit()
        case .breakTime:
            Label(pomodoro.timeString, systemImage: "cup.and.saucer.fill")
                .monospacedDigit()
        case .idle:
            if appState.todayRemainingCount > 0 {
                Label("\(appState.todayRemainingCount)", systemImage: "checklist")
            } else {
                Image(systemName: "checklist")
            }
        }
    }
}
