package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/UncertaintyDeterminesYou4ndMe/hibi/internal/todo"
	"github.com/UncertaintyDeterminesYou4ndMe/hibi/locales"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// defaultHotkey opens the panel from any app. ⌃Space and ⌃⌥Space switch
// input sources and ⌥Space opens the ChatGPT and Claude apps, so it stays
// off those. settings.json "hotkey" overrides it; "off" turns it off.
const defaultHotkey = "Ctrl+Alt+H"

type app struct {
	store    *todo.Store
	pomodoro *Pomodoro
	focusLog *focusLog

	// trayTitle, trayMood and trayTip are the title, the icon and the
	// tooltip last sent to the tray, so a tick that leaves them unchanged
	// does not wake the main thread.
	trayTitle atomic.Value
	trayMood  atomic.Int32
	trayTip   atomic.Value
	// sitting is what the presence monitor last said about sitting, for the
	// cat's mood.
	sitting atomic.Value

	panelMu          sync.RWMutex
	tray             *mygo.Tray
	panel            *mygo.Window
	prompt           *mygo.Window
	promptAllowClose *atomic.Bool

	quitting   atomic.Bool
	panelShown atomic.Bool
	// panelDrawn tells that a frame of the panel was built while it showed
	// (drawWhenShown).
	panelDrawn    atomic.Bool
	editingActive atomic.Bool
	reloadPending atomic.Bool
	watchCancel   context.CancelFunc
	deskMotion    presenceHold
	// activityAware is autoSense.ActivityAware, read off the UI goroutine;
	// activityHold holds presence while a video plays or a call goes on.
	activityAware atomic.Bool
	activityHold  presenceHold
	sitWaiting    atomic.Bool // a sitting reminder waits for a call to end
	deskAPIStop   func()

	statusMu sync.RWMutex
	status   string

	// The presence monitor runs only while a setting needs it
	// (applyAutoSense).
	presenceMu     sync.Mutex
	presenceCancel context.CancelFunc
	presence       *presenceMonitor

	// View state is touched by MyGo's main thread.
	quickAdd       string
	quickAddHandle ui.Handle
	activeTag      string
	showHelp       bool
	showSettings   bool
	// autoSense, awayIndex (into awayPresets) and sitIndex (into sitPresets)
	// back the settings view's
	// controls, which write them between frames.
	autoSense    autoSense
	awayIndex    int
	sitIndex     int
	editingID    uint64
	editingTier  todo.Tier
	editText     string
	editHandle   ui.Handle
	editFocused  bool
	editStarting bool
	editSubmit   bool // Enter was pressed in the edit
	// The keyboard's selection in the task list (listKeys); selID 0 is none.
	listHandle  ui.Handle
	listFocused bool
	selTier     todo.Tier
	selID       uint64
	selScroll   bool
}

func newApp(store *todo.Store) *app {
	a := &app{store: store}
	if dir, err := configDir(); err == nil {
		a.focusLog = openFocusLog(filepath.Join(dir, "focus.json"))
	}
	a.pomodoro = NewPomodoro(store, a, loadPreferences().focusMinutes())
	a.pomodoro.busy = currentBusy
	return a
}

func (a *app) ready() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID: "hibi", Icon: trayIconPNG, IconIsTemplate: true,
		ToolTip: locales.TrayTooltip(),
	})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	a.panelMu.Lock()
	a.tray = tray
	a.panelMu.Unlock()
	a.store.SetOnChange(a.TimerChanged)
	a.TimerChanged()

	if runtime.GOOS == "linux" {
		tray.SetMenu(a.trayMenu())
	} else {
		tray.OnClick(a.togglePanel)
		tray.OnRightClick(func() {
			a.closePanel()
			tray.SetMenu(a.trayMenu())
			defer tray.SetMenu(nil)
			tray.PopUpMenu()
		})
	}
	// The panel and the prompt say their texts anew each frame; the menu
	// bar and the Linux menu were set once.
	mygo.App.OnLocaleChanged(func(string) {
		if runtime.GOOS == "linux" {
			tray.SetMenu(a.trayMenu())
		}
		a.TimerChanged() // the title and tooltip it last set differ now
	})
	a.startWatching()
	a.startPresence()
	a.registerHotkey()
	a.deskAPIStop = a.startDeskAPI()
}

// trayMenu is the menu of the menu bar icon, in the app's language.
func (a *app) trayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: locales.MenuOpenHibi(), Click: func(*mygo.MenuItem, *mygo.Window) { a.togglePanel() }},
		{Label: locales.MenuOpenTodos(), Click: func(*mygo.MenuItem, *mygo.Window) { a.openTodoFile() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
}

func (a *app) registerHotkey() {
	hotkey := loadPreferences().Hotkey
	if hotkey == "" {
		hotkey = defaultHotkey
	}
	if hotkey == "off" {
		return
	}
	if err := mygo.GlobalShortcut.Register(hotkey, a.togglePanel); err != nil {
		a.setStatus(locales.StatusHotkeyUnavailable(hotkey, err.Error()))
	}
}

// startPresence starts the timer automatically when the user is at the
// computer and ends the session when they leave, as the settings say; see
// presenceMonitor.
func (a *app) startPresence() {
	// The poll may be a minute away; these cannot wait for it.
	mygo.Power.OnResume(a.CheckPresence)
	mygo.Power.OnUnlockScreen(a.CheckPresence)
	a.applyAutoSense(loadPreferences().autoSense())
}

// applyAutoSense hands the auto-sense settings to the timer, and runs the
// presence monitor only while one of them needs it: with both off, Hibi
// does not read the idle time at all. Main thread.
func (a *app) applyAutoSense(s autoSense) {
	a.autoSense = s
	a.activityAware.Store(s.ActivityAware)
	a.awayIndex = slices.Index(awayPresets, s.AwayMinutes)
	a.sitIndex = slices.Index(sitPresets, s.SitMinutes)
	a.pomodoro.SetAutoSense(s)
	a.presenceMu.Lock()
	defer a.presenceMu.Unlock()
	if !s.watches() {
		if a.presenceCancel != nil {
			a.presenceCancel()
		}
		a.presence, a.presenceCancel = nil, nil
		a.sitting.Store(sitting{}) // nobody counts the sitting now
		a.refreshTray()
		return
	}
	threshold := time.Duration(s.AwayMinutes) * time.Minute
	if a.presence != nil {
		a.presence.sitEvery.Store(int64(s.sitEvery()))
		a.presence.SetThreshold(threshold) // polls at once with the new settings
		return
	}
	idle := func() time.Duration {
		return a.activityHold.held(a.deskMotion.idle(systemIdle()), a.activityAware.Load(), watchingOrInCall)
	}
	a.presence = newPresenceMonitor(idle, a.pomodoro)
	a.presence.threshold.Store(int64(threshold))
	a.presence.sitEvery.Store(int64(s.sitEvery()))
	a.presence.onSit = a.sitTooLong
	a.presence.onSitPeriods = a.sitPeriodsChanged
	ctx, cancel := context.WithCancel(context.Background())
	a.presenceCancel = cancel
	go a.presence.run(ctx)
}

// setAutoSense applies changed auto-sense settings and saves them.
func (a *app) setAutoSense(s autoSense) {
	a.applyAutoSense(s)
	prefs := loadPreferences()
	prefs.AutoSense = &autoSensePrefs{
		AutoStart: &s.AutoStart, EndOnAway: &s.EndOnAway, AwayMinutes: s.AwayMinutes,
		SitReminder: &s.SitReminder, SitMinutes: s.SitMinutes, ActivityAware: &s.ActivityAware,
	}
	if err := savePreferences(prefs); err != nil {
		a.setStatus(locales.StatusSettingsNotSaved(err.Error()))
	}
}

// sitTooLong reminds the user to get up and walk. It runs on the presence
// monitor's goroutine. The walk is shorter than the away threshold, so a
// focus session that runs goes on. In a call the reminder waits for the
// call to end (callDeferLimit at most), then comes unless the user already
// got up.
func (a *app) sitTooLong(sat time.Duration) {
	if !a.activityAware.Load() || !microphoneInUse() {
		a.showSitReminder(sat)
		return
	}
	if !a.sitWaiting.CompareAndSwap(false, true) {
		return // the reminder that waits already says it
	}
	go func() {
		defer a.sitWaiting.Store(false)
		start := time.Now()
		for microphoneInUse() && time.Since(start) < callDeferLimit {
			time.Sleep(30 * time.Second)
		}
		// The call held presence (applyAutoSense's idle): listening without
		// input is still sitting.
		if a.activityHold.idle(a.deskMotion.idle(systemIdle())) < sitBreak {
			a.showSitReminder(sat + time.Since(start))
		}
	}()
}

func (a *app) showSitReminder(sat time.Duration) {
	a.presenceMu.Lock()
	away := defaultAwayThreshold
	if a.presence != nil {
		away = time.Duration(a.presence.threshold.Load())
	}
	a.presenceMu.Unlock()
	body := locales.SitNotifyBody()
	if a.pomodoro.Snapshot().Phase == phaseFocus && away > sitBreak {
		body = locales.SitNotifyBodyFocus()
	}
	title := locales.SitNotifyTitle(int(sat / time.Minute))
	go func() {
		n := mygo.NewNotification(mygo.NotificationOptions{ID: "hibi-sit", Title: title, Body: body, Group: "hibi-sit"})
		if err := n.Show(); err != nil {
			log.Printf("hibi: sitting reminder: %v", err)
		}
	}()
}

func (a *app) stopPresence() {
	a.presenceMu.Lock()
	defer a.presenceMu.Unlock()
	if a.presenceCancel != nil {
		a.presenceCancel()
	}
	a.presence, a.presenceCancel = nil, nil
}

func (a *app) CheckPresence() {
	a.presenceMu.Lock()
	defer a.presenceMu.Unlock()
	if a.presence != nil {
		a.presence.Kick()
	}
}

func (a *app) currentTray() *mygo.Tray {
	a.panelMu.RLock()
	defer a.panelMu.RUnlock()
	return a.tray
}

func (a *app) currentPanel() *mygo.Window {
	a.panelMu.RLock()
	defer a.panelMu.RUnlock()
	return a.panel
}

// togglePanel shows the panel, or closes it when it shows.
//
// It asks the popover whether it shows, not panelShown: a popover that
// went without OnHide (AppKit did not show it, or closed it on its own)
// leaves panelShown set, and Hide does nothing to a popover that does not
// show, so every click would only "close" it and the panel never came
// back.
func (a *app) togglePanel() {
	panel := a.currentPanel()
	if panel != nil && panel.IsVisible() {
		panel.Close() // hides it, as OnClose in newPanel decides
		return
	}
	if a.panelShown.Swap(false) {
		a.resetPanelState()
	}
	a.showPanel()
}

// openPanel shows the panel unless it shows already, as Hibi is opened
// again while it runs. Main thread only.
func (a *app) openPanel() {
	if a.currentTray() == nil {
		return // not ready yet
	}
	if panel := a.currentPanel(); panel != nil && panel.IsVisible() {
		return
	}
	a.togglePanel()
}

func (a *app) showPanel() {
	a.reloadFromDisk()
	mygo.App.Show() // hiding the panel last time may have hidden Hibi
	panel := a.currentPanel()
	if panel == nil {
		panel = a.newPanel()
	}
	a.pomodoro.SetFineTicks(true) // the panel shows the seconds
	a.panelShown.Store(true)
	a.panelDrawn.Store(false)
	panel.Show()
	// A popover's window exists once it shows. Floating keeps it above
	// ordinary windows without taking the input method's popup level.
	panel.SetAlwaysOnTop(true)
	a.quickAddHandle.Focus()
	go a.drawWhenShown(panel)
}

// drawWhenShown asks the panel for a frame every refresh until one builds
// while the panel shows, for a second at most. MyGo builds no frame for a
// window out of sight: it holds the frame until the system says that the
// window shows again. It hears that for its own windows, but not for the
// window a popover makes as it opens. So the panel opened with the frame
// it last showed, with the time of then, until the timer's next tick
// asked for a frame, up to a second later. Reported as egoist/mygo#2053:
// once MyGo draws a popover's held frame as it shows, this can go.
func (a *app) drawWhenShown(panel *mygo.Window) {
	for range 60 {
		time.Sleep(16 * time.Millisecond)
		if a.panelDrawn.Load() || !a.panelShown.Load() {
			return
		}
		panel.Invalidate()
	}
}

// newPanel creates the panel, hidden. Hibi makes one and shows it again
// each time: on macOS 26 every NSPopover that has shown keeps its
// material's backdrop (a 4.5 MB graphics surface) after it is released,
// so a new popover per click grew Hibi by that much each time. Closing
// the panel (a click elsewhere, Escape, the icon, the shortcut) hides it.
func (a *app) newPanel() *mygo.Window {
	panel := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray: a.currentTray(), Placement: mygo.PlacementBottom, Gap: 6,
		Width: panelWidth, Height: 620, FitContent: true,
		MinWidth: panelWidth, MaxWidth: panelWidth, MinHeight: 220, MaxHeight: 680,
		Focusable: true, Hidden: true, Shadow: true,
		// The system popover draws its material, rounded corners, shadow
		// and the arrow to the tray icon; the view skips its own paper
		// where ui.Context.Vibrancy says the material shows.
		Popover: true,
		Content: ui.View(a.panelView),
	})
	a.panelMu.Lock()
	a.panel = panel
	a.panelMu.Unlock()
	panel.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting.Load() {
			return
		}
		e.PreventDefault()
		// Not from inside AppKit's own close (popoverShouldClose:).
		go panel.Hide()
	})
	panel.OnHide(a.panelHidden)
	// Hibi quitting closes it for good.
	panel.OnClosed(func() {
		a.panelMu.Lock()
		if a.panel == panel {
			a.panel = nil
		}
		a.panelMu.Unlock()
		a.panelHidden()
	})
	return panel
}

// panelHidden runs on the main thread once the panel no longer shows.
func (a *app) panelHidden() {
	if !a.panelShown.Swap(false) {
		return
	}
	a.panelMu.RLock()
	prompt := a.prompt
	a.panelMu.RUnlock()
	a.resetPanelState()
	// The panel's frames leave free memory behind in the Go heap and in
	// malloc; Hibi then idles in the menu bar for hours, so hand it back
	// once the close has settled.
	time.AfterFunc(2*time.Second, func() {
		debug.FreeOSMemory()
		releaseNativeMemory()
	})
	// Showing the panel activated Hibi. Closed by Escape or the shortcut,
	// it would leave Hibi in front with no window, and the keys typed next
	// would go nowhere: hiding gives the keyboard back to the app before.
	// A click elsewhere already moved it; the break prompt must stay.
	if prompt == nil && !a.quitting.Load() && mygo.App.IsActive() {
		mygo.App.Hide()
	}
}

// resetPanelState ends what the shown panel had going: an edit, a
// selection and the timer's per-second ticks.
func (a *app) resetPanelState() {
	a.editingActive.Store(false)
	a.editingID = 0
	a.clearSelection()
	a.pomodoro.SetFineTicks(false)
}

func (a *app) closePanel() {
	if panel := a.currentPanel(); panel != nil && a.panelShown.Load() {
		panel.Close()
	}
}

// TimerChanged also handles store changes. MyGo hops window/tray updates to
// the main thread, so ticker and watcher goroutines may call it directly.
func (a *app) TimerChanged() {
	a.refreshTray()
	if panel := a.currentPanel(); panel != nil && a.panelShown.Load() {
		panel.Update(func() {})
	}
}

// refreshTray brings the menu bar title, icon and tooltip up to date. The
// title and the icon change with the phase, the day's focus and the
// sitting; the timer ticks every second. SetTitle, SetIcon and SetToolTip
// wait for the main thread, so it skips repeats.
func (a *app) refreshTray() {
	tray := a.currentTray()
	if tray == nil {
		return
	}
	remaining, _, _ := todo.TodayCounts(a.store.Snapshot(), "")
	timer := a.pomodoro.Snapshot()
	title := trayTitle(timer, remaining)
	if previous, ok := a.trayTitle.Swap(title).(string); !ok || previous != title {
		tray.SetTitle(title)
	}
	sat, _ := a.sitting.Load().(sitting)
	today := time.Duration(a.todayFocusSeconds(timer)) * time.Second
	mood, tip := catMoodFor(timer.Phase, today, sat)
	if tip == "" {
		tip = locales.TrayTooltip()
	}
	if catMood(a.trayMood.Swap(int32(mood))) != mood {
		if err := tray.SetIcon(trayIcon(mood), true); err != nil {
			log.Print("tray icon: ", err)
		}
	}
	if previous, ok := a.trayTip.Swap(tip).(string); !ok || previous != tip {
		tray.SetToolTip(tip)
	}
}

// sitPeriodsChanged is the presence monitor telling how long the user has
// sat, on its goroutine.
func (a *app) sitPeriodsChanged(periods int, sitEvery time.Duration) {
	a.sitting.Store(sitting{periods, sitEvery})
	a.refreshTray()
}

// trayTitle is the menu bar text beside the icon. It shows no time left: a
// running focus shows the icon alone, and a pause or a break only its name,
// so the title changes when the phase does. With no timer it shows today's
// open tasks.
func trayTitle(s pomodoroSnapshot, openToday int) string {
	switch s.Phase {
	case phaseFocus:
		return ""
	case phasePaused:
		return locales.TrayPaused()
	case phaseBreak:
		return locales.TrayBreak()
	case phaseBreakPending:
		return locales.BreakDue()
	}
	if openToday > 0 {
		return strconv.Itoa(openToday)
	}
	return ""
}

// todayFocusSeconds is today's recorded focus plus the running session.
func (a *app) todayFocusSeconds(timer pomodoroSnapshot) int {
	return a.focusLog.Seconds(dayKey(time.Now())) + timer.SessionElapsedSeconds
}

func (a *app) startWatching() {
	ctx, cancel := context.WithCancel(context.Background())
	a.watchCancel = cancel
	watcher, err := mygo.WatchFiles(ctx, a.store.Path(), mygo.FileWatchOptions{Debounce: 80 * time.Millisecond})
	if err != nil {
		a.setStatus(locales.StatusWatchUnavailable(err.Error()))
		return
	}
	go func() {
		for {
			_, err := watcher.Next(ctx)
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					log.Printf("hibi: watch: %v", err)
				}
				return
			}
			a.reloadFromDisk()
		}
	}()
}

func (a *app) reloadFromDisk() {
	if a.editingActive.Load() {
		a.reloadPending.Store(true)
		return
	}
	if err := a.store.Reload(); err != nil {
		a.setStatus(locales.StatusReadFailed(err.Error()))
	}
}

func (a *app) setStatus(status string) {
	a.statusMu.Lock()
	a.status = status
	a.statusMu.Unlock()
	if status != "" {
		log.Print("hibi: ", status)
	}
	a.TimerChanged()
}

func (a *app) statusText() string {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

func (a *app) perform(err error) {
	if errors.Is(err, todo.ErrEmptyText) {
		a.setStatus(locales.ErrEmptyText())
	} else if err != nil {
		a.setStatus(err.Error())
	} else {
		a.setStatus("")
	}
}

func (a *app) openTodoFile() {
	go func() {
		if err := mygo.Shell.OpenPath(a.store.Path()); err != nil {
			a.setStatus(locales.StatusOpenFailed(err.Error()))
		}
	}()
}

// timerHost implementation.
func (a *app) Chime() { mygo.Shell.Beep() }

func (a *app) Notify(title, body string) {
	go func() {
		if err := mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body, Group: "hibi-pomodoro"}).Show(); err != nil {
			log.Printf("hibi: notification: %v", err)
		}
	}()
}

func (a *app) ShowBreakPrompt(focusMinutes, restMinutes int) {
	a.HideBreakPrompt()
	// Closing the panel may have hidden Hibi; a hidden app shows no window.
	mygo.App.Show()
	prompt := mygo.NewWindow(mygo.WindowOptions{
		Title: locales.PromptTitle(), Width: 320, Height: 260,
		DisableResize: true, DisableMinimize: true, DisableMaximize: true,
		DisableFullScreen: true, AlwaysOnTop: true, SkipTaskbar: true, Panel: true, Frameless: true,
		BackgroundColor: "light-dark(#f5f5f3, #111113)",
		Content:         ui.View(a.breakPromptView),
	})
	allowClose := &atomic.Bool{}
	a.panelMu.Lock()
	a.prompt = prompt
	a.promptAllowClose = allowClose
	a.panelMu.Unlock()
	prompt.SetVisibleOnAllWorkspaces(true)
	prompt.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting.Load() && !allowClose.Load() {
			e.PreventDefault()
		}
	})
	prompt.OnClosed(func() {
		a.panelMu.Lock()
		if a.prompt == prompt {
			a.prompt = nil
			a.promptAllowClose = nil
		}
		a.panelMu.Unlock()
	})
	prompt.Focus()
}

func (a *app) HideBreakPrompt() {
	a.panelMu.Lock()
	prompt := a.prompt
	allowClose := a.promptAllowClose
	a.prompt = nil
	a.promptAllowClose = nil
	a.panelMu.Unlock()
	if allowClose != nil {
		allowClose.Store(true)
	}
	if prompt != nil {
		prompt.Close()
	}
}

func (a *app) BringBreakPromptToFront() {
	a.panelMu.RLock()
	prompt := a.prompt
	a.panelMu.RUnlock()
	if prompt != nil {
		prompt.Show()
		prompt.Focus()
	}
}

func (a *app) SaveFocusMinutes(minutes int) {
	prefs := loadPreferences()
	prefs.FocusMinutes = minutes
	if err := savePreferences(prefs); err != nil {
		log.Printf("hibi: save focus preference: %v", err)
	}
}

func (a *app) RecordFocus(seconds int) {
	if err := a.focusLog.Add(dayKey(time.Now()), seconds); err != nil {
		log.Printf("hibi: record focus: %v", err)
	}
}

// preferences is settings.json. Saving rewrites every field, so a field
// the user set by hand survives the app saving another.
type preferences struct {
	FocusMinutes int             `json:"focusMinutes,omitempty"`
	Hotkey       string          `json:"hotkey,omitempty"`
	DeskAPI      *deskAPIPrefs   `json:"deskApi,omitempty"`
	AutoSense    *autoSensePrefs `json:"autoSense,omitempty"`
}

// autoSensePrefs is the "autoSense" block of settings.json. A field left
// out keeps its default (defaultAutoSense).
type autoSensePrefs struct {
	AutoStart   *bool `json:"autoStart,omitempty"`
	EndOnAway   *bool `json:"endOnAway,omitempty"`
	AwayMinutes int   `json:"awayMinutes,omitempty"`
	SitReminder *bool `json:"sitReminder,omitempty"`
	SitMinutes  int   `json:"sitMinutes,omitempty"`
	// ActivityAware is autoSense.ActivityAware.
	ActivityAware *bool `json:"activityAware,omitempty"`
}

func (p preferences) autoSense() autoSense {
	s := defaultAutoSense()
	if a := p.AutoSense; a != nil {
		if a.AutoStart != nil {
			s.AutoStart = *a.AutoStart
		}
		if a.EndOnAway != nil {
			s.EndOnAway = *a.EndOnAway
		}
		if slices.Contains(awayPresets, a.AwayMinutes) {
			s.AwayMinutes = a.AwayMinutes
		}
		if a.SitReminder != nil {
			s.SitReminder = *a.SitReminder
		}
		if slices.Contains(sitPresets, a.SitMinutes) {
			s.SitMinutes = a.SitMinutes
		}
		if a.ActivityAware != nil {
			s.ActivityAware = *a.ActivityAware
		}
	}
	return s
}

func (p preferences) focusMinutes() int {
	if !slices.Contains(focusPresets, p.FocusMinutes) {
		return 40
	}
	return p.FocusMinutes
}

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "Hibi"), err
}

func preferencesPath() (string, error) {
	dir, err := configDir()
	return filepath.Join(dir, "settings.json"), err
}

func loadPreferences() preferences {
	var prefs preferences
	if path, err := preferencesPath(); err == nil {
		if contents, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(contents, &prefs)
		}
	}
	return prefs
}

func savePreferences(prefs preferences) error {
	path, err := preferencesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(contents, '\n'), 0o644)
}
