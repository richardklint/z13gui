// SPDX-License-Identifier: Apache-2.0

package gui

// tabs.go — the main view: header (title, telemetry, rotation lock,
// on-screen keyboard, close), the tab bar, and a three-page stack (Power,
// Lighting, Display).
//
// Built for touch first: every target is at least 48px and a switch row
// toggles from anywhere on the row. The tab bar, header buttons and every row
// are also in a per-tab gamepad focus grid; the shoulder buttons switch tabs.

import (
	"fmt"
	"strconv"

	"github.com/dahui/z13gui/internal/system"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const (
	tabPower    = "power"
	tabLighting = "lighting"
	tabDisplay  = "display"

	// headerRow and navRow come before every tab's content rows (1 and up),
	// matching the layout: header, then the tab bar, then the page.
	headerRow = -2
	navRow    = -1
)

var tabOrder = []string{tabPower, tabLighting, tabDisplay}

var tabMeta = map[string]struct{ label, icon string }{
	tabPower:    {"Power", "z13-power-symbolic"},
	tabLighting: {"Lighting", "z13-lighting-symbolic"},
	tabDisplay:  {"Display", "z13-display-symbolic"},
}

// switchRow is a full-width button holding a title, a subtitle and a switch.
// The switch is display-only (not targetable): tapping anywhere on the row
// flips it, which is what makes it usable with a finger — and, because the row
// is a gtk.Button, it needs no addTouchActivate workaround under gamescope.
type switchRow struct {
	btn   *gtk.Button
	sw    *gtk.Switch
	title *gtk.Label
	sub   *gtk.Label
}

func (w *Window) newSwitchRow(title, sub string, onChange func(bool)) *switchRow {
	r := &switchRow{}
	r.btn = gtk.NewButton()
	r.btn.AddCSSClass("switch-row")
	box := gtk.NewBox(gtk.OrientationHorizontal, 12)
	text := gtk.NewBox(gtk.OrientationVertical, 2)
	text.SetHExpand(true)
	text.SetVAlign(gtk.AlignCenter)
	r.title = gtk.NewLabel(title)
	r.title.SetXAlign(0)
	r.title.AddCSSClass("row-title")
	r.sub = gtk.NewLabel(sub)
	r.sub.SetXAlign(0)
	r.sub.SetWrap(true)
	r.sub.AddCSSClass("row-sub")
	r.sub.SetVisible(sub != "")
	text.Append(r.title)
	text.Append(r.sub)
	r.sw = gtk.NewSwitch()
	r.sw.SetVAlign(gtk.AlignCenter)
	r.sw.SetCanTarget(false)
	r.sw.SetFocusable(false)
	r.sw.ConnectStateSet(func(state bool) bool {
		if !w.syncing {
			onChange(state)
		}
		return false
	})
	box.Append(text)
	box.Append(r.sw)
	r.btn.SetChild(box)
	r.btn.ConnectClicked(func() { r.sw.SetActive(!r.sw.Active()) })
	return r
}

func (r *switchRow) setSub(s string) {
	r.sub.SetLabel(s)
	r.sub.SetVisible(s != "")
}

func (r *switchRow) focus(row int, section string, vis func() bool) focusItem {
	return focusItem{
		widget: r.btn, row: row, col: 0, section: section, isVisible: vis,
		onActivate: func() { r.btn.Activate() },
	}
}

// card wraps children in a rounded surface. Rows are separated by a hairline.
func card(children ...gtk.Widgetter) *gtk.Box {
	c := gtk.NewBox(gtk.OrientationVertical, 0)
	c.AddCSSClass("card")
	c.SetOverflow(gtk.OverflowHidden) // clip row hover backgrounds to the rounded corners
	for i, ch := range children {
		if i > 0 {
			sep := gtk.NewSeparator(gtk.OrientationHorizontal)
			sep.AddCSSClass("card-sep")
			c.Append(sep)
		}
		c.Append(ch)
	}
	return c
}

func iconButton(icon, tooltip, class string) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.SetTooltipText(tooltip)
	b.AddCSSClass("header-btn")
	if class != "" {
		b.AddCSSClass(class)
	}
	return b
}

func pageBox() *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVertical, 10)
	b.SetMarginTop(12)
	b.SetMarginBottom(16)
	b.SetMarginStart(12)
	b.SetMarginEnd(12)
	return b
}

// buildMainView builds the header, tab pages and tab bar.
func (w *Window) buildMainView() *gtk.Box {
	main := gtk.NewBox(gtk.OrientationVertical, 0)
	main.Append(w.buildHeader())

	w.tabStack = gtk.NewStack()
	w.tabStack.SetTransitionType(gtk.StackTransitionTypeNone)
	w.tabStack.SetVExpand(true)
	pages := map[string]*gtk.Box{
		tabPower:    w.buildPowerPage(),
		tabLighting: w.buildLightingPage(),
		tabDisplay:  w.buildDisplayPage(),
	}
	for _, t := range tabOrder {
		scroll := newDrawerScroll(pages[t])
		w.tabScrolls[t] = scroll
		w.tabStack.AddNamed(scroll, t)
	}
	// Tab bar under the header, above the pages.
	main.Append(w.buildTabBar())
	main.Append(w.tabStack)

	w.buildTabFocusLists()
	w.selectTab(w.prefs.Tab)
	return main
}

func (w *Window) buildHeader() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 6)
	row.AddCSSClass("drawer-header")

	text := gtk.NewBox(gtk.OrientationVertical, 1)
	text.SetHExpand(true)
	text.SetVAlign(gtk.AlignCenter)
	titleRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	title := gtk.NewLabel("Z13")
	title.SetXAlign(0)
	title.AddCSSClass("header-title")
	// Shown while Feral GameMode has a game registered.
	w.gameBadge = gtk.NewLabel("Game mode")
	w.gameBadge.AddCSSClass("game-badge")
	w.gameBadge.SetVAlign(gtk.AlignCenter)
	w.gameBadge.SetVisible(false)
	titleRow.Append(title)
	titleRow.Append(w.gameBadge)
	w.headerTelemetry = gtk.NewLabel("")
	w.headerTelemetry.SetXAlign(0)
	w.headerTelemetry.AddCSSClass("header-telemetry")
	text.Append(titleRow)
	text.Append(w.headerTelemetry)
	row.Append(text)

	// Quick rotation lock: the same setting as the Display tab's switch, one
	// tap from any tab. Hidden until the probe finds kscreen-doctor.
	w.rotBtn = iconButton("z13-rotate-symbolic", "Auto-rotate is on — tap to lock", "")
	w.rotBtn.SetVisible(false)
	w.rotBtn.ConnectClicked(func() {
		locked := !w.rotRow.sw.Active()
		w.syncRotBtn(locked) // immediate feedback; the display read-back confirms
		w.rotRow.sw.SetActive(locked)
	})
	row.Append(w.rotBtn)

	w.oskBtn = iconButton("z13-keyboard-symbolic", "On-screen keyboard", "")
	w.oskBtn.SetVisible(false) // shown once the probe finds KWin's keyboard
	w.oskBtn.ConnectClicked(func() { w.toggleKeyboard() })
	row.Append(w.oskBtn)

	w.closeBtn = iconButton("z13-close-symbolic", "Close", "close-btn")
	w.closeBtn.ConnectClicked(func() { w.hide() })
	row.Append(w.closeBtn)
	return row
}

func (w *Window) buildTabBar() *gtk.Box {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.AddCSSClass("tab-bar")
	bar.SetHomogeneous(true)
	for _, t := range tabOrder {
		t := t
		meta := tabMeta[t]
		b := gtk.NewButton()
		b.AddCSSClass("tab-bar-btn")
		inner := gtk.NewBox(gtk.OrientationVertical, 4)
		inner.SetHAlign(gtk.AlignCenter)
		inner.SetVAlign(gtk.AlignCenter)
		pill := gtk.NewBox(gtk.OrientationHorizontal, 0)
		pill.AddCSSClass("tab-pill")
		pill.SetHAlign(gtk.AlignCenter)
		img := gtk.NewImageFromIconName(meta.icon)
		img.SetHExpand(true)
		pill.Append(img)
		lbl := gtk.NewLabel(meta.label)
		lbl.AddCSSClass("tab-label")
		inner.Append(pill)
		inner.Append(lbl)
		b.SetChild(inner)
		b.ConnectClicked(func() { w.selectTab(t) })
		w.tabBtns[t] = b
		bar.Append(b)
	}
	return bar
}

// selectTab shows tab t, marks its bar button and swaps in its focus grid.
func (w *Window) selectTab(t string) {
	if _, ok := tabMeta[t]; !ok {
		t = tabPower
	}
	w.curTab = t
	w.tabStack.SetVisibleChildName(t)
	for name, b := range w.tabBtns {
		if name == t {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
	w.mainFocusItems = w.tabFocus[t]
	if w.viewStack != nil && w.viewStack.VisibleChildName() == "main" {
		w.swapFocusList(w.mainFocusItems)
	}
	if w.prefs.Tab != t {
		w.prefs.Tab = t
		w.savePrefs()
	}
}

// cycleTab moves to the next (dir > 0) or previous tab, wrapping.
func (w *Window) cycleTab(dir int) {
	idx := 0
	for i, t := range tabOrder {
		if t == w.curTab {
			idx = i
		}
	}
	idx = (idx + dir + len(tabOrder)) % len(tabOrder)
	w.selectTab(tabOrder[idx])
}

// --- Power page ---

func (w *Window) buildPowerPage() *gtk.Box {
	page := pageBox()
	page.Append(w.buildPowerCard())

	page.Append(w.buildProfileSection())
	page.Append(w.buildBatteryCard())
	return page
}

func (w *Window) buildPowerCard() *gtk.Box {
	c := gtk.NewBox(gtk.OrientationVertical, 6)
	c.AddCSSClass("card")
	c.AddCSSClass("card-padded")

	top := gtk.NewBox(gtk.OrientationHorizontal, 6)
	w.powerValue = gtk.NewLabel("–")
	w.powerValue.AddCSSClass("power-value")
	unit := gtk.NewLabel("W")
	unit.AddCSSClass("power-unit")
	unit.SetVAlign(gtk.AlignBaseline)
	w.powerValue.SetVAlign(gtk.AlignBaseline)
	caption := gtk.NewLabel("APU power")
	caption.AddCSSClass("row-sub")
	caption.SetHExpand(true)
	caption.SetXAlign(1)
	caption.SetVAlign(gtk.AlignBaseline)
	top.Append(w.powerValue)
	top.Append(unit)
	top.Append(caption)
	c.Append(top)

	stats := gtk.NewBox(gtk.OrientationHorizontal, 0)
	stats.SetHomogeneous(true)
	w.cpuTempLbl = statLabel(stats)
	w.gpuTempLbl = statLabel(stats)
	w.fanLbl = statLabel(stats)
	c.Append(stats)
	return c
}

func statLabel(parent *gtk.Box) *gtk.Label {
	l := gtk.NewLabel("")
	l.SetXAlign(0)
	l.SetUseMarkup(true)
	l.AddCSSClass("stat")
	parent.Append(l)
	return l
}

// statPair builds a two-line "Health / 90%" cell.
func statPair(parent *gtk.Box, name string) *gtk.Label {
	box := gtk.NewBox(gtk.OrientationVertical, 1)
	n := gtk.NewLabel(name)
	n.SetXAlign(0)
	n.AddCSSClass("row-sub")
	v := gtk.NewLabel("–")
	v.SetXAlign(0)
	v.AddCSSClass("stat-value")
	box.Append(n)
	box.Append(v)
	parent.Append(box)
	return v
}

func (w *Window) buildBatteryCard() *gtk.Box {
	head := gtk.NewBox(gtk.OrientationHorizontal, 12)
	head.AddCSSClass("card-block")
	w.battLevel = gtk.NewLevelBarForInterval(0, 100)
	w.battLevel.AddCSSClass("batt-level")
	w.battLevel.SetVAlign(gtk.AlignCenter)
	w.battLevel.SetValue(0)
	headText := gtk.NewBox(gtk.OrientationVertical, 1)
	headText.SetHExpand(true)
	w.battPctLbl = gtk.NewLabel("–")
	w.battPctLbl.SetXAlign(0)
	w.battPctLbl.AddCSSClass("batt-pct")
	w.battStatusLbl = gtk.NewLabel("")
	w.battStatusLbl.SetXAlign(0)
	w.battStatusLbl.AddCSSClass("row-sub")
	headText.Append(w.battPctLbl)
	headText.Append(w.battStatusLbl)
	head.Append(w.battLevel)
	head.Append(headText)

	stats := gtk.NewBox(gtk.OrientationHorizontal, 6)
	stats.SetHomogeneous(true)
	stats.AddCSSClass("card-block")
	stats.AddCSSClass("card-block-tight")
	w.battHealthLbl = statPair(stats, "Health")
	w.battCapLbl = statPair(stats, "Capacity")
	w.battRateLbl = statPair(stats, "Rate")

	top := gtk.NewBox(gtk.OrientationVertical, 0)
	top.Append(head)
	top.Append(stats)

	w.limitRow = w.newSwitchRow("Charge limit", "", w.setLimitEnabled)

	w.limitPresetBox = gtk.NewBox(gtk.OrientationHorizontal, 8)
	w.limitPresetBox.AddCSSClass("btn-group")
	w.limitPresetBox.AddCSSClass("card-block")
	w.limitPresetBox.SetHomogeneous(true)
	for _, v := range system.LimitPresets {
		v := v
		b := gtk.NewButtonWithLabel(strconv.Itoa(v) + "%")
		b.ConnectClicked(func() { w.setLimitPreset(v) })
		w.limitBtns[v] = b
		w.limitPresetBox.Append(b)
	}

	w.restoreRow = w.newSwitchRow("Re-enable after unplugging", "Full charge is a one-off top-up", w.setRestoreOnUnplug)

	limitBlock := gtk.NewBox(gtk.OrientationVertical, 0)
	limitBlock.Append(w.limitRow.btn)
	limitBlock.Append(w.limitPresetBox)
	return card(top, limitBlock, w.restoreRow.btn)
}

// --- Lighting page ---

func (w *Window) buildLightingPage() *gtk.Box {
	page := pageBox()
	page.Append(w.buildTabRow())
	page.Append(w.buildModeSection())

	w.color1 = w.newColorInput("FF0000", "color1-swatch", "COLOR 1")
	w.color2 = w.newColorInput("000000", "color2-swatch", "COLOR 2")
	w.updateSwatches()
	w.color1Box = colorSubBox("COLOR 1", w.color1.row)
	w.color2Box = colorSubBox("COLOR 2", w.color2.row)
	page.Append(w.color1Box)
	page.Append(w.color2Box)

	w.speedBox = w.buildSpeedBox()
	page.Append(w.speedBox)
	w.brightBox = w.buildBrightnessBox()
	page.Append(w.brightBox)
	w.syncModeVis()
	return page
}

// --- Display page ---

// orientations lists the orientation buttons in grid order.
var orientations = []struct {
	rot   int
	label string
	class string // rotates the orientation icon to match
}{
	{system.RotationNormal, "Landscape", "orient-0"},
	{system.RotationLeft, "Portrait", "orient-270"},
	{system.RotationInverted, "Landscape (flipped)", "orient-180"},
	{system.RotationRight, "Portrait (flipped)", "orient-90"},
}

var rotatePolicies = []struct{ arg, label string }{
	{"inTabletMode", "Tablet"},
	{"always", "Always"},
}

func (w *Window) buildDisplayPage() *gtk.Box {
	page := pageBox()

	// Brightness + refresh rate.
	bright := gtk.NewBox(gtk.OrientationVertical, 8)
	bright.AddCSSClass("card-block")
	bHead := gtk.NewBox(gtk.OrientationHorizontal, 0)
	bTitle := gtk.NewLabel("Brightness")
	bTitle.AddCSSClass("row-title")
	bTitle.SetHExpand(true)
	bTitle.SetXAlign(0)
	w.screenBrightLbl = gtk.NewLabel("")
	w.screenBrightLbl.AddCSSClass("row-value")
	bHead.Append(bTitle)
	bHead.Append(w.screenBrightLbl)
	w.screenBright = gtk.NewScaleWithRange(gtk.OrientationHorizontal, brightnessStep, 100, brightnessStep)
	w.screenBright.SetDrawValue(false)
	w.screenBright.SetFocusable(false)
	w.screenBright.AddCSSClass("big-scale")
	w.screenBright.ConnectValueChanged(w.onScreenBrightness)
	w.screenBright.SetHExpand(true)
	w.screenBright.SetVAlign(gtk.AlignCenter)
	bright.Append(bHead)
	bright.Append(w.screenBright)
	w.screenBrightBox = bright

	// One line: title on the left, the rate buttons on the right.
	rate := gtk.NewBox(gtk.OrientationHorizontal, 8)
	rate.AddCSSClass("card-block")
	rTitle := gtk.NewLabel("Refresh rate")
	rTitle.AddCSSClass("row-title")
	rTitle.SetXAlign(0)
	rTitle.SetHExpand(true)
	w.rateRow = gtk.NewBox(gtk.OrientationHorizontal, 6)
	w.rateRow.AddCSSClass("btn-group")
	w.rateRow.AddCSSClass("compact")
	w.rateRow.SetHomogeneous(true)
	w.rateRow.SetVAlign(gtk.AlignCenter)
	rate.Append(rTitle)
	rate.Append(w.rateRow)
	w.rateBox = rate

	w.screenCard = card(bright, rate)
	w.screenCard.SetVisible(false)
	page.Append(w.screenCard)

	// Rotation.
	w.rotRow = w.newSwitchRow("Rotation lock", "", w.setRotationLock)

	// Orientations: one row of icon buttons. A single screen-with-up-arrow
	// icon, rotated in CSS (.orient-*), shows each orientation; the tooltip
	// names it.
	orient := gtk.NewBox(gtk.OrientationHorizontal, 6)
	orient.SetHomogeneous(true)
	orient.AddCSSClass("btn-group")
	for _, o := range orientations {
		o := o
		b := gtk.NewButtonFromIconName("z13-orient-symbolic")
		b.SetTooltipText(o.label)
		b.AddCSSClass("orient-btn")
		b.AddCSSClass(o.class)
		b.ConnectClicked(func() { w.setOrientation(o.rot) })
		w.orientBtns[o.rot] = b
		orient.Append(b)
	}
	// Auto-rotate policy on one line: label left, choices right.
	pol := gtk.NewBox(gtk.OrientationHorizontal, 8)
	polLabel := gtk.NewLabel("Auto-rotate")
	polLabel.SetXAlign(0)
	polLabel.SetHExpand(true)
	polLabel.AddCSSClass("row-sub")
	polBtns := gtk.NewBox(gtk.OrientationHorizontal, 6)
	polBtns.SetHomogeneous(true)
	polBtns.AddCSSClass("btn-group")
	polBtns.AddCSSClass("compact")
	polBtns.SetVAlign(gtk.AlignCenter)
	for _, p := range rotatePolicies {
		p := p
		b := gtk.NewButtonWithLabel(p.label)
		b.SetTooltipText("Auto-rotate when unlocked: " + p.label)
		b.ConnectClicked(func() { w.setRotatePolicy(p.arg) })
		w.policyBtns[p.arg] = b
		polBtns.Append(b)
	}
	pol.Append(polLabel)
	pol.Append(polBtns)
	rotBody := gtk.NewBox(gtk.OrientationVertical, 10)
	rotBody.AddCSSClass("card-block")
	rotBody.AddCSSClass("card-block-tight")
	rotBody.Append(orient)
	rotBody.Append(pol)

	rotTop := gtk.NewBox(gtk.OrientationVertical, 0)
	rotTop.Append(w.rotRow.btn)
	rotTop.Append(rotBody)
	w.rotCard = card(rotTop)
	w.rotCard.SetVisible(false)
	page.Append(w.rotCard)

	// Panel and system toggles.
	od := w.newSwitchRow("Panel overdrive", "Faster pixel response, may ghost", func(on bool) {
		w.sendOverdriveSet(boolInt(on))
	})
	w.overdriveSwitch = od.sw
	w.overdriveRow = od
	bs := w.newSwitchRow("Boot sound", "Chime at power-on", func(on bool) {
		w.sendBootSoundSet(boolInt(on))
	})
	w.bootSoundSwitch = bs.sw
	w.bootSoundRow = bs

	// Theme opens a view rather than toggling a setting, so it reads as a
	// button: text centred on the button's full width. A CenterBox keeps it
	// truly centred — the chevron at the end does not push it left.
	w.themeRow = gtk.NewButton()
	w.themeRow.AddCSSClass("switch-row")
	tBox := gtk.NewCenterBox()
	tText := gtk.NewBox(gtk.OrientationVertical, 2)
	tText.SetVAlign(gtk.AlignCenter)
	tTitle := centeredLabel("Theme")
	tTitle.AddCSSClass("row-title")
	w.themeNameLbl = centeredLabel("")
	w.themeNameLbl.AddCSSClass("row-sub")
	tText.Append(tTitle)
	tText.Append(w.themeNameLbl)
	tBox.SetCenterWidget(tText)
	chevron := gtk.NewImageFromIconName("z13-chevron-symbolic")
	chevron.SetVAlign(gtk.AlignCenter)
	tBox.SetEndWidget(chevron)
	w.themeRow.SetChild(tBox)
	w.themeRow.ConnectClicked(func() { w.showThemeView() })
	w.updateThemeName()

	page.Append(card(od.btn, bs.btn, w.themeRow))
	return page
}

// brightnessStep is the grid the screen brightness slider snaps to, in percent.
const brightnessStep = 5

// centeredLabel is a label that stays centred in whatever button holds it.
func centeredLabel(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetHExpand(true)
	l.SetHAlign(gtk.AlignCenter)
	l.SetJustify(gtk.JustifyCenter)
	return l
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- focus grids ---

func (w *Window) headerFocus() []focusItem {
	return []focusItem{
		{widget: w.rotBtn, row: headerRow, col: 0, section: "header",
			onActivate: func() { w.rotBtn.Activate() }},
		{widget: w.oskBtn, row: headerRow, col: 1, section: "header",
			onActivate: func() { w.oskBtn.Activate() }},
		{widget: w.closeBtn, row: headerRow, col: 2, section: "header",
			onActivate: func() { w.hide() }},
	}
}

func (w *Window) navFocus() []focusItem {
	items := make([]focusItem, 0, len(tabOrder))
	for i, t := range tabOrder {
		t := t
		items = append(items, focusItem{
			widget: w.tabBtns[t], row: navRow, col: i, section: "nav",
			onActivate: func() { w.selectTab(t) },
		})
	}
	return items
}

func visibleFn(wd gtk.Widgetter) func() bool {
	return func() bool { return gtk.BaseWidget(wd).IsVisible() }
}

// buildTabFocusLists rebuilds every tab's focus grid. Called again whenever a
// tab's set of controls changes (refresh-rate buttons appear after the display
// probe), so the grid never holds a destroyed widget.
func (w *Window) buildTabFocusLists() {
	w.tabFocus = map[string][]focusItem{
		tabPower:    w.powerFocus(),
		tabLighting: w.lightingFocus(),
		tabDisplay:  w.displayFocus(),
	}
	if w.curTab != "" {
		w.mainFocusItems = w.tabFocus[w.curTab]
		if w.viewStack != nil && w.viewStack.VisibleChildName() == "main" {
			w.swapFocusList(w.mainFocusItems)
		}
	}
}

func (w *Window) powerFocus() []focusItem {
	items := w.headerFocus()
	for i, p := range profiles {
		btn := w.profileBtns[p]
		items = append(items, focusItem{
			widget: btn, row: 3 + i/2, col: i % 2, section: "profile",
			onActivate: func() { btn.Activate() },
		})
	}
	items = append(items, w.limitRow.focus(5, "battery", nil))
	for i, v := range system.LimitPresets {
		btn := w.limitBtns[v]
		items = append(items, focusItem{
			widget: btn, row: 6, col: i, section: "battery",
			isVisible:  func() bool { return btn.IsSensitive() },
			onActivate: func() { btn.Activate() },
		})
	}
	items = append(items, w.restoreRow.focus(7, "battery", nil))
	items = append(items, w.navFocus()...)
	return append(items, w.errBarFocusItem())
}

func (w *Window) lightingFocus() []focusItem {
	items := w.headerFocus()
	for col, btn := range []*gtk.CheckButton{w.tabKB, w.tabLB} {
		btn := btn
		items = append(items, focusItem{
			widget: btn, row: 1, col: col, section: "tabs",
			onActivate: func() { btn.SetActive(true) },
		})
	}
	for i, m := range modeOrder {
		btn := w.modeButtons[m]
		items = append(items, focusItem{
			widget: btn, row: 2 + i/3, col: i % 3, section: "mode",
			onActivate: func() { btn.Activate() },
		})
	}
	for n, ci := range []struct {
		in  *colorInput
		box *gtk.Box
	}{{w.color1, w.color1Box}, {w.color2, w.color2Box}} {
		if ci.in == nil {
			continue
		}
		in := ci.in
		vis := visibleFn(ci.box)
		row := 4 + n*2
		for col, btn := range in.presetBtns {
			btn := btn
			items = append(items, focusItem{
				widget: btn, row: row, col: col, section: fmt.Sprintf("color%d", n+1),
				isVisible: vis, onActivate: func() { btn.Activate() },
			})
		}
		items = append(items, focusItem{
			widget: in.customBtn, row: row + 1, col: 0, section: fmt.Sprintf("color%d", n+1),
			isVisible: vis, onActivate: func() { w.showColorView(in) },
		})
	}
	for col, s := range speeds {
		btn := w.speedBtns[s]
		items = append(items, focusItem{
			widget: btn, row: 8, col: col, section: "speed",
			isVisible: visibleFn(w.speedBox), onActivate: func() { btn.Activate() },
		})
	}
	brLeft, brRight, brGet, brSet := scaleAdjust(w.brightScale, 1)
	items = append(items, focusItem{
		widget: w.brightScale, row: 9, col: 0, section: "brightness",
		isVisible: visibleFn(w.brightBox), editable: true,
		onLeft: brLeft, onRight: brRight, getValue: brGet, setValue: brSet,
	})
	items = append(items, w.navFocus()...)
	return append(items, w.errBarFocusItem())
}

func (w *Window) displayFocus() []focusItem {
	items := w.headerFocus()
	screenVis := visibleFn(w.screenCard)
	brightVis := func() bool { return screenVis() && w.screenBrightBox.IsVisible() }
	sl, sr, sg, ss := scaleAdjust(w.screenBright, brightnessStep)
	items = append(items, focusItem{widget: w.screenBright, row: 1, col: 0, section: "screen",
		isVisible: brightVis, editable: true, onLeft: sl, onRight: sr, getValue: sg, setValue: ss})
	for i, r := range w.rates {
		btn := w.rateBtns[r.ModeID]
		items = append(items, focusItem{
			widget: btn, row: 2, col: i, section: "screen",
			isVisible:  func() bool { return screenVis() && w.rateBox.IsVisible() },
			onActivate: func() { btn.Activate() },
		})
	}
	rotVis := visibleFn(w.rotCard)
	items = append(items, w.rotRow.focus(3, "rotation", rotVis))
	for i, o := range orientations {
		btn := w.orientBtns[o.rot]
		items = append(items, focusItem{
			widget: btn, row: 4, col: i, section: "rotation",
			isVisible: rotVis, onActivate: func() { btn.Activate() },
		})
	}
	for i, p := range rotatePolicies {
		btn := w.policyBtns[p.arg]
		items = append(items, focusItem{
			widget: btn, row: 5, col: i, section: "rotation",
			isVisible: rotVis, onActivate: func() { btn.Activate() },
		})
	}
	items = append(items,
		w.overdriveRow.focus(7, "system", nil),
		w.bootSoundRow.focus(8, "system", nil),
		focusItem{widget: w.themeRow, row: 9, col: 0, section: "system",
			onActivate: func() { w.showThemeView() }},
	)
	items = append(items, w.navFocus()...)
	return append(items, w.errBarFocusItem())
}
