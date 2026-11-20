// SPDX-License-Identifier: Apache-2.0

package gui

// features.go — behaviour behind the redesign's controls: the GameMode badge,
// the charge-limit switch and its one-off top-up, the power card, and the
// Display tab (brightness, refresh rate, rotation, on-screen keyboard).
//
// Follows the package rules: widget reads on the GTK thread, every daemon call
// or command in a goroutine, results back through glib.IdleAdd, failures to
// the error bar. Decisions are in internal/system.

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13gui/internal/colorconv"
	"github.com/dahui/z13gui/internal/daemon"
	"github.com/dahui/z13gui/internal/system"
	"github.com/dahui/z13gui/internal/theme"
	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// powerHistorySamples is one minute at the 1 s sample rate.
const powerHistorySamples = 60

func (w *Window) savePrefs() { system.SavePrefs(w.prefs) }

// startPowerSampler records APU power once a second for the life of the
// process, so the graph already holds a minute of history when the drawer
// opens. One sysfs read per second; the goroutine touches only its own copies.
func startPowerSampler(s sensors, h *system.History) {
	if s.apuPower == "" {
		return
	}
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			if v, ok := s.readAPUWatts(); ok {
				h.Add(v)
			}
		}
	}()
}

// capsReprobeEvery throttles re-probing for integrations still missing. On a
// desktop that will never have them (GNOME, gamescope) it bounds the cost to a
// few short commands per five seconds of drawer use.
const capsReprobeEvery = 5 * time.Second

// probeDesktop checks for the desktop integrations not yet found and shows
// the matching rows. Runs at startup and again on show while any are missing
// (see maybeReprobe), off the GTK thread.
func (w *Window) probeDesktop() {
	if w.capsProbing {
		return
	}
	w.capsProbing = true
	w.capsProbedAt = time.Now()
	have := w.caps
	first := !w.capsProbedOnce // always log the startup result
	w.capsProbedOnce = true
	go func() {
		c := probeDesktopCaps(have)
		glib.IdleAdd(func() {
			w.capsProbing = false
			if c == w.caps && !first {
				return
			}
			slog.Info("desktop integrations", "kscreen", c.kscreen, "brightness", c.brightness,
				"osk", c.osk, "gamemode", c.gamemode)
			w.applyCaps(c)
		})
	}()
}

// maybeReprobe re-runs the probe on show while an integration is missing.
func (w *Window) maybeReprobe() {
	if system.ReprobeDue(w.caps.complete(), w.capsProbedAt, time.Now(), capsReprobeEvery) {
		w.probeDesktop()
	}
}

func (w *Window) applyCaps(c desktopCaps) {
	w.caps = c
	w.oskBtn.SetVisible(c.osk)
	w.screenBrightBox.SetVisible(c.brightness)
	w.rateBox.SetVisible(c.kscreen)
	w.screenCard.SetVisible(c.brightness || c.kscreen)
	w.rotCard.SetVisible(c.kscreen)
	if w.visible.Load() {
		w.refreshDesktop()
	}
}

// refreshDesktop re-reads everything the daemon does not report: display
// configuration, screen brightness, GPU level and the sensors. Called on show.
func (w *Window) refreshDesktop() {
	c := w.caps
	sens := w.sens
	go func() {
		r := readPowerReadings(sens, c.gamemode)
		glib.IdleAdd(func() { w.applyPowerReadings(r) })
	}()
	if c.kscreen {
		go w.rereadDisplay()
	}
	if c.brightness {
		go func() {
			cur, max, err := readBrightness()
			if err != nil {
				slog.Debug("brightness read failed", "err", err)
				return
			}
			glib.IdleAdd(func() {
				w.brightMax = max
				w.syncing = true
				w.screenBright.SetValue(float64(cur) * 100 / float64(max))
				w.syncing = false
				w.screenBrightLbl.SetLabel(fmt.Sprintf("%d%%", int(w.screenBright.Value())))
			})
		}()
	}
}

// --- header and power card ---

// powerReadings is everything the power card reads from sysfs, gathered off
// the GTK thread.
type powerReadings struct {
	gpuTemp  int
	gpuOK    bool
	batt     system.Battery
	battOK   bool
	acOnline bool
	acKnown  bool
	gameMode bool // Feral GameMode has a game registered
}

func readPowerReadings(s sensors, gamemode bool) powerReadings {
	var r powerReadings
	if gamemode {
		n, err := gameModeClients()
		r.gameMode = err == nil && n > 0
	}
	r.gpuTemp, r.gpuOK = s.readGPUTemp()
	r.batt, r.battOK = s.readBattery()
	r.acOnline, r.acKnown = s.acOnline()
	return r
}

func (w *Window) applyPowerReadings(r powerReadings) {
	if r.battOK {
		w.lastBatt = &r.batt
	}
	w.updateHeader()
	w.gameBadge.SetVisible(r.gameMode)

	samples := w.powerHist.Snapshot()
	if len(samples) > 0 {
		w.powerValue.SetLabel(system.FormatWatts(samples[len(samples)-1]))
	}
	w.powerGraph.QueueDraw()

	if w.state != nil {
		w.cpuTempLbl.SetMarkup(fmt.Sprintf("CPU <b>%d°C</b>", w.state.Temperature))
		w.fanLbl.SetMarkup(fmt.Sprintf("Fan <b>%d rpm</b>", w.state.FanRPM))
	}
	if r.gpuOK {
		w.gpuTempLbl.SetMarkup(fmt.Sprintf("GPU <b>%d°C</b>", r.gpuTemp))
	}

	if !r.battOK {
		return
	}
	b := r.batt
	w.battLevel.SetValue(float64(b.Capacity))
	w.battPctLbl.SetLabel(fmt.Sprintf("%d%%", b.Capacity))
	w.battStatusLbl.SetLabel(b.StatusLine(r.acOnline))
	if h := b.HealthPercent(); h >= 0 {
		w.battHealthLbl.SetLabel(fmt.Sprintf("%d%%", h))
	}
	if b.EnergyFull > 0 && b.EnergyDesign > 0 {
		w.battCapLbl.SetLabel(fmt.Sprintf("%.0f / %.0f Wh", float64(b.EnergyFull)/1e6, float64(b.EnergyDesign)/1e6))
	}
	w.battRateLbl.SetLabel(b.RateLabel())
}

func (w *Window) updateHeader() {
	s := ""
	if w.state != nil {
		s = fmt.Sprintf("%d°C · %d RPM", w.state.Temperature, w.state.FanRPM)
	}
	if w.lastBatt != nil {
		if s != "" {
			s += " · "
		}
		s += fmt.Sprintf("%d%%", w.lastBatt.Capacity)
	}
	w.headerTelemetry.SetLabel(s)
}

// newPowerGraph is the 60 s APU power sparkline. Painted with Cairo, so like
// the fan curve it applies the backend scale and the theme accent itself.
func (w *Window) newPowerGraph() *gtk.DrawingArea {
	da := gtk.NewDrawingArea()
	da.AddCSSClass("power-graph")
	da.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		s := 1.0
		if w.backend != nil && w.backend.Scale() > 0 {
			s = w.backend.Scale()
		}
		wd, ht := float64(width), float64(height)
		ar, ag, ab, ok := colorconv.RGB(w.colors.Accent)
		if !ok {
			ar, ag, ab, _ = colorconv.RGB(theme.DefaultColors.Accent)
		}
		br, bg, bb, ok := colorconv.RGB(w.colors.Border)
		if !ok {
			br, bg, bb = 0.3, 0.3, 0.3
		}

		// Midline, dashed.
		cr.SetSourceRGBA(br, bg, bb, 1)
		cr.SetLineWidth(1 * s)
		cr.SetDash([]float64{3 * s, 4 * s}, 0)
		cr.MoveTo(0, ht/2)
		cr.LineTo(wd, ht/2)
		cr.Stroke()
		cr.SetDash(nil, 0)

		samples := w.powerHist.Snapshot()
		max := system.GraphScale(samples, 30, 10)
		pad := 2 * s // keep the 2px line inside the box at the extremes
		pts := system.GraphPoints(samples, w.powerHist.Cap(), wd, ht-2*pad, max)
		if len(pts) < 2 {
			return
		}
		for i := range pts {
			pts[i].Y += pad
		}

		cr.SetSourceRGBA(ar, ag, ab, 0.18)
		cr.MoveTo(pts[0].X, ht)
		for _, p := range pts {
			cr.LineTo(p.X, p.Y)
		}
		cr.LineTo(pts[len(pts)-1].X, ht)
		cr.ClosePath()
		cr.Fill()

		cr.SetSourceRGBA(ar, ag, ab, 1)
		cr.SetLineWidth(2 * s)
		cr.SetLineJoin(cairo.LineJoinRound)
		cr.MoveTo(pts[0].X, pts[0].Y)
		for _, p := range pts[1:] {
			cr.LineTo(p.X, p.Y)
		}
		cr.Stroke()
	})
	return da
}

// --- charge limit ---

// syncBattery mirrors the daemon's limit into the switch and presets. The
// daemon reports 100 while the limit is off.
func (w *Window) syncBattery() {
	if w.limitRow == nil {
		return
	}
	limit := 0
	if w.state != nil {
		limit = w.state.Battery
	}
	active := system.LimitActive(limit)
	if limit > 0 {
		w.limitRow.sw.SetActive(active)
	}
	for v, b := range w.limitBtns {
		if active && v == limit {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
	w.limitPresetBox.SetSensitive(active)
	switch {
	case active:
		w.limitRow.setSub(fmt.Sprintf("Stops at %d%% to protect the battery", limit))
	case limit == 0:
		w.limitRow.setSub("")
	case w.prefs.PendingRestore:
		w.limitRow.setSub("Charging to 100% until you unplug")
	default:
		w.limitRow.setSub("Off · charging to 100%")
	}
	w.restoreRow.sw.SetActive(w.prefs.RestoreOnUnplug)
}

func (w *Window) setLimitEnabled(on bool) {
	val := system.FullCharge
	if on {
		val = system.PreferredLimit(w.prefs.ChargeLimit)
		w.prefs.PendingRestore = false
	} else {
		w.prefs.PendingRestore = w.prefs.RestoreOnUnplug
	}
	w.savePrefs()
	w.limitPresetBox.SetSensitive(on)
	w.sendBatteryLimit(val)
}

func (w *Window) setLimitPreset(v int) {
	w.prefs.ChargeLimit = v
	w.prefs.PendingRestore = false
	w.savePrefs()
	for k, b := range w.limitBtns {
		if k == v {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
	w.sendBatteryLimit(v)
}

func (w *Window) setRestoreOnUnplug(on bool) {
	w.prefs.RestoreOnUnplug = on
	limitOff := w.state != nil && w.state.Battery > 0 && !system.LimitActive(w.state.Battery)
	w.prefs.PendingRestore = on && limitOff
	w.savePrefs()
	w.syncing = true
	w.syncBattery()
	w.syncing = false
}

func (w *Window) sendBatteryLimit(val int) {
	go func() {
		slog.Debug("sendBatteryLimitSet: calling daemon", "limit", val)
		if err := daemon.Err(api.SendBatteryLimitSet(val)); err != nil {
			w.reportError("Set charge limit", err)
			return
		}
		w.clearErrorAsync()
		w.refreshState()
	}()
}

// checkChargeRestore ends a one-off full charge once the charger is unplugged.
// Called on the daemon's power-source event and once at startup. z13gui runs as
// a user service for the whole session, so this works with the drawer closed.
func (w *Window) checkChargeRestore() {
	if !w.prefs.PendingRestore {
		return
	}
	sens := w.sens
	limit := system.PreferredLimit(w.prefs.ChargeLimit)
	go func() {
		// The event can arrive a moment before the adapter's online flag flips.
		time.Sleep(time.Second)
		online, known := sens.acOnline()
		if !system.ShouldRestore(true, known, online) {
			return
		}
		slog.Info("charger unplugged after a full charge; re-enabling the charge limit", "limit", limit)
		err := daemon.Err(api.SendBatteryLimitSet(limit))
		glib.IdleAdd(func() {
			if err != nil {
				w.reportError("Re-enable charge limit", err)
				return
			}
			w.prefs.PendingRestore = false
			w.savePrefs()
		})
		w.refreshState()
	}()
}

// --- on-screen keyboard ---

// toggleKeyboard closes the drawer first so the keyboard attaches to the
// window underneath — opened while the drawer has focus, it would type into
// the drawer, which has no text field.
func (w *Window) toggleKeyboard() {
	w.hide()
	go func() {
		time.Sleep(350 * time.Millisecond) // let the slide-out finish and focus return
		if err := toggleOnScreenKeyboard(); err != nil {
			slog.Warn("on-screen keyboard toggle failed", "err", err)
		}
	}()
}

// --- display ---

func (w *Window) onScreenBrightness() {
	pct := int(w.screenBright.Value())
	w.screenBrightLbl.SetLabel(fmt.Sprintf("%d%%", pct))
	if w.syncing || w.brightMax <= 0 {
		return
	}
	if w.brightTimer != nil {
		w.brightTimer.Stop()
	}
	max := w.brightMax
	w.brightTimer = time.AfterFunc(80*time.Millisecond, func() {
		raw := pct * max / 100
		if err := setBrightness(raw); err != nil {
			w.reportError("Set brightness", err)
		}
	})
}

// stepScreenBrightness moves the slider to the next multiple of
// brightnessStep in direction dir (see system.SnapStep); the slider's own
// handler debounces and applies it, so a run of taps sends one change.
func (w *Window) stepScreenBrightness(dir int) {
	adj := w.screenBright.Adjustment()
	adj.SetValue(system.SnapStep(adj.Value(), brightnessStep, brightnessMinMove, dir, adj.Lower(), adj.Upper()))
}

func (w *Window) rereadDisplay() {
	d, err := readDisplay()
	if err != nil {
		slog.Debug("display read failed", "err", err)
		return
	}
	glib.IdleAdd(func() { w.applyDisplay(d) })
}

func orientationLabel(rot int) string {
	for _, o := range orientations {
		if o.rot == rot {
			return o.label
		}
	}
	return "current orientation"
}

func (w *Window) applyDisplay(d system.Display) {
	w.display = d
	w.syncing = true
	defer func() { w.syncing = false }()

	locked := d.Policy == system.PolicyNever
	w.rotRow.sw.SetActive(locked)
	switch {
	case locked:
		w.rotRow.setSub("Held at " + orientationLabel(d.Rotation))
	case d.Policy == system.PolicyAlways:
		w.rotRow.setSub("Screen always follows the sensor")
	default:
		w.rotRow.setSub("Screen follows the sensor in tablet mode")
	}
	for rot, b := range w.orientBtns {
		if rot == d.Rotation {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
	pol := w.prefs.AutoRotate
	if !locked {
		pol = system.PolicyArg(d.Policy)
	}
	for arg, b := range w.policyBtns {
		if arg == pol {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}

	w.setRates(system.QuickRates(d.Rates), d.CurrentModeID, d.CurrentHz)
}

// setRates rebuilds the refresh-rate buttons when the set changes and marks
// the current one.
func (w *Window) setRates(rates []system.Rate, currentID string, currentHz int) {
	same := len(rates) == len(w.rates)
	for i := 0; same && i < len(rates); i++ {
		same = rates[i] == w.rates[i]
	}
	if !same {
		for c := w.rateRow.FirstChild(); c != nil; c = w.rateRow.FirstChild() {
			w.rateRow.Remove(c)
		}
		w.rates = rates
		w.rateBtns = make(map[string]*gtk.Button, len(rates))
		for i, r := range rates {
			r := r
			hint := "Smooth"
			if i == 0 && len(rates) > 1 {
				hint = "Battery saver"
			}
			b := gtk.NewButton()
			box := gtk.NewBox(gtk.OrientationVertical, 0)
			box.SetHAlign(gtk.AlignCenter)
			box.SetVAlign(gtk.AlignCenter)
			hz := centeredLabel(strconv.Itoa(r.Hz) + " Hz")
			hz.AddCSSClass("rate-hz")
			h := centeredLabel(hint)
			h.AddCSSClass("rate-hint")
			box.Append(hz)
			box.Append(h)
			b.SetChild(box)
			b.ConnectClicked(func() { w.setRefresh(r) })
			w.rateBtns[r.ModeID] = b
			w.rateRow.Append(b)
		}
		w.buildTabFocusLists()
	}
	for _, r := range w.rates {
		b := w.rateBtns[r.ModeID]
		if r.ModeID == currentID || r.Hz == currentHz {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
	w.rateBox.SetVisible(w.caps.kscreen && len(w.rates) > 1)
}

// kscreenApply applies want in a goroutine, reports a failure, and re-reads
// the display either way so the widgets show the truth.
func (w *Window) kscreenApply(op string, want system.DisplayWant) {
	name := w.display.Name
	go func() {
		if _, err := kscreenSet(name, want); err != nil {
			w.reportError(op, err)
		} else {
			w.clearErrorAsync()
		}
		w.rereadDisplay()
	}()
}

func (w *Window) setRefresh(r system.Rate) {
	for id, b := range w.rateBtns {
		if id == r.ModeID {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
	w.kscreenApply("Set refresh rate", system.DisplayWant{Policy: system.KeepPolicy, ModeID: r.ModeID})
}

func (w *Window) setRotationLock(on bool) {
	pol := system.PolicyFromArg(w.prefs.AutoRotate)
	if on {
		pol = system.PolicyNever
	}
	w.kscreenApply("Set rotation lock", system.DisplayWant{Policy: pol})
}

// setOrientation rotates the panel and locks it there: with auto-rotate still
// on, the sensor would undo the choice as soon as the device moved.
func (w *Window) setOrientation(rot int) {
	if system.RotationArg(rot) == "" {
		return
	}
	w.kscreenApply("Rotate display", system.DisplayWant{Policy: system.PolicyNever, Rotation: rot})
}

func (w *Window) setRotatePolicy(arg string) {
	w.prefs.AutoRotate = arg
	w.savePrefs()
	for a, b := range w.policyBtns {
		if a == arg {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
	if !w.rotRow.sw.Active() {
		w.kscreenApply("Set auto-rotate", system.DisplayWant{Policy: system.PolicyFromArg(arg)})
	}
}

// --- theme row ---

func (w *Window) updateThemeName() {
	if w.themeNameLbl == nil {
		return
	}
	if w.isCustomTheme || w.customCSSTheme {
		w.themeNameLbl.SetLabel("Custom")
		return
	}
	cfg := theme.LoadAppConfig()
	id, _ := cfg.Effective(w.sysDark)
	name := theme.BuiltinName(id)
	if cfg.FollowSystem {
		name = "Following system · " + name
	}
	w.themeNameLbl.SetLabel(name)
}
