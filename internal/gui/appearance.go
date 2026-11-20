// SPDX-License-Identifier: Apache-2.0

package gui

// appearance.go — following the desktop's light/dark preference.
//
// The scheme comes from the XDG desktop portal (org.freedesktop.appearance
// color-scheme), which KDE, GNOME and most others implement. It is read once
// at startup and then tracked through the portal's SettingChanged signal, so
// the drawer re-themes the moment the system flips — no polling.

import (
	"context"
	"log/slog"

	"github.com/dahui/z13gui/internal/system"
	"github.com/dahui/z13gui/internal/theme"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const (
	portalDest   = "org.freedesktop.portal.Desktop"
	portalPath   = "/org/freedesktop/portal/desktop"
	portalIface  = "org.freedesktop.portal.Settings"
	appearanceNS = "org.freedesktop.appearance"
	schemeKey    = "color-scheme"
)

func readColorScheme() (uint32, bool) {
	out, err := runCmd("busctl", "--user", "call", portalDest, portalPath, portalIface,
		"ReadOne", "ss", appearanceNS, schemeKey)
	if err != nil {
		slog.Debug("portal colour scheme unavailable", "err", err)
		return 0, false
	}
	return system.ParseColorScheme(out)
}

// watchAppearance reads the current scheme and subscribes to changes. The
// subscription's callback runs on the GTK main context.
func (w *Window) watchAppearance() {
	go func() {
		if s, ok := readColorScheme(); ok {
			glib.IdleAdd(func() { w.setSystemDark(system.SchemeIsDark(s)) })
		}
	}()

	conn, err := gio.BusGetSync(context.Background(), gio.BusTypeSession)
	if err != nil {
		slog.Warn("no session bus; the theme will not follow the system appearance", "err", err)
		return
	}
	w.sessionBus = conn // keep the connection (and with it the subscription) alive
	conn.SignalSubscribe(portalDest, portalIface, "SettingChanged", portalPath, appearanceNS,
		gio.DBusSignalFlagsNone,
		func(_ *gio.DBusConnection, _, _, _, _ string, params *glib.Variant) {
			// SettingChanged(s namespace, s key, v value)
			if params == nil || params.NChildren() < 3 || params.ChildValue(1).String() != schemeKey {
				return
			}
			v := params.ChildValue(2)
			if v.TypeString() == "v" {
				v = v.Variant()
			}
			if v.TypeString() != "u" {
				return
			}
			w.setSystemDark(system.SchemeIsDark(v.Uint32()))
		})
}

func (w *Window) setSystemDark(dark bool) {
	if w.schemeKnown && dark == w.sysDark {
		return
	}
	w.schemeKnown = true
	w.sysDark = dark
	slog.Info("system appearance", "dark", dark)
	if theme.LoadAppConfig().FollowSystem {
		w.applyEffectiveTheme()
	}
	w.syncThemePicker()
}

// applyEffectiveTheme shows the theme the config selects for the current
// system appearance. A theme.toml/theme.css override wins over everything.
func (w *Window) applyEffectiveTheme() {
	if w.isCustomTheme || w.customCSSTheme {
		return
	}
	id, ac := theme.LoadAppConfig().Effective(w.sysDark)
	w.renderTheme(id, ac)
}

// --- theme view: follow switch and light/dark slots ---

// buildFollowCard is the top of the theme view: the follow-system switch and,
// while it is on, the Light/Dark slot buttons choosing which slot the theme
// list below edits.
func (w *Window) buildFollowCard() *gtk.Box {
	w.followRow = w.newSwitchRow("Follow system appearance", "", w.setFollowSystem)

	w.slotBox = gtk.NewBox(gtk.OrientationHorizontal, 8)
	w.slotBox.AddCSSClass("btn-group")
	w.slotBox.AddCSSClass("card-block")
	w.slotBox.SetHomogeneous(true)
	w.slotBtns = map[bool]*gtk.Button{}
	w.slotNameLbls = map[bool]*gtk.Label{}
	for _, dark := range []bool{false, true} {
		dark := dark
		b := gtk.NewButton()
		box := gtk.NewBox(gtk.OrientationVertical, 0)
		box.SetHAlign(gtk.AlignCenter)
		box.SetVAlign(gtk.AlignCenter)
		title := "Light"
		if dark {
			title = "Dark"
		}
		t := centeredLabel(title)
		t.AddCSSClass("rate-hz")
		n := centeredLabel("")
		n.AddCSSClass("rate-hint")
		box.Append(t)
		box.Append(n)
		b.SetChild(box)
		b.ConnectClicked(func() {
			w.themeSlotDark = dark
			w.syncThemePicker()
		})
		w.slotBtns[dark] = b
		w.slotNameLbls[dark] = n
		w.slotBox.Append(b)
	}

	c := card(w.followRow.btn)
	c.Append(w.slotBox)
	c.SetMarginBottom(8)
	return c
}

func (w *Window) setFollowSystem(on bool) {
	cfg := theme.LoadAppConfig()
	cfg.FollowSystem = on
	if on {
		// Seed the slots from the fixed theme when it fits one, so turning this on
		// keeps the current look for the current mode.
		if cfg.LightTheme == "" {
			cfg.LightTheme = theme.DefaultLightTheme
		}
		if cfg.DarkTheme == "" {
			cfg.DarkTheme = theme.DefaultDarkTheme
		}
		if c, ok := theme.BuiltinByID(cfg.Theme); ok {
			cfg.SetSlot(theme.IsDark(c), cfg.Theme, cfg.Accent)
		}
		w.themeSlotDark = w.sysDark
	}
	theme.SaveAppConfig(cfg)
	w.applyEffectiveTheme()
	w.syncThemePicker()
}

// slotChoice is the theme the list should show as selected: the edited slot's
// when following the system, otherwise the fixed theme.
func (w *Window) slotChoice(cfg theme.AppConfig) (id, accent string) {
	if !cfg.FollowSystem {
		return cfg.Theme, cfg.Accent
	}
	return cfg.Effective(w.themeSlotDark)
}

// syncThemePicker brings the theme view in line with the config: the follow
// switch, the slot buttons and which theme/accent is marked. Safe before the
// view is built.
func (w *Window) syncThemePicker() {
	w.updateThemeName()
	if w.followRow == nil {
		return
	}
	cfg := theme.LoadAppConfig()
	prev := w.syncing
	w.syncing = true
	defer func() { w.syncing = prev }()

	w.followRow.sw.SetActive(cfg.FollowSystem)
	mode := "light"
	if w.sysDark {
		mode = "dark"
	}
	if cfg.FollowSystem {
		w.followRow.setSub("System is " + mode + " · pick a theme for each mode")
	} else {
		w.followRow.setSub("Use one theme in light and dark mode")
	}
	w.slotBox.SetVisible(cfg.FollowSystem)
	for dark, b := range w.slotBtns {
		id, _ := cfg.Effective(dark)
		w.slotNameLbls[dark].SetLabel(theme.BuiltinName(id))
		if dark == w.themeSlotDark {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}

	id, accent := w.slotChoice(cfg)
	for i, t := range theme.Builtins {
		if i >= len(w.themeRadios) {
			break
		}
		if t.ID != id {
			continue
		}
		w.themeRadios[i].SetActive(true)
		var dot *gtk.Button
		for j, a := range t.Accents {
			if a.ID == accent && i < len(w.themeDots) && j < len(w.themeDots[i]) {
				dot = w.themeDots[i][j]
			}
		}
		w.setActiveAccentDot(dot)
	}
}

// --- GameMode badge ---

const (
	gameModeDest  = "com.feralinteractive.GameMode"
	gameModePath  = "/com/feralinteractive/GameMode"
	gameModeIface = "com.feralinteractive.GameMode"
)

// watchGameMode keeps the header badge in step with Feral GameMode through its
// PropertiesChanged signal (ClientCount is emits-change), so nothing polls.
// One read at startup and one per show (refreshGameMode) cover a daemon that
// started, restarted or was missed while the signal could not be delivered.
func (w *Window) watchGameMode() {
	w.refreshGameMode()
	if w.sessionBus == nil {
		return
	}
	w.sessionBus.SignalSubscribe(gameModeDest, "org.freedesktop.DBus.Properties", "PropertiesChanged",
		gameModePath, gameModeIface, gio.DBusSignalFlagsNone,
		func(_ *gio.DBusConnection, _, _, _, _ string, params *glib.Variant) {
			// PropertiesChanged(s interface, a{sv} changed, as invalidated)
			if params == nil || params.NChildren() < 2 {
				return
			}
			changed := params.ChildValue(1)
			for i := uint(0); i < changed.NChildren(); i++ {
				entry := changed.ChildValue(i)
				if entry.ChildValue(0).String() != "ClientCount" {
					continue
				}
				v := entry.ChildValue(1)
				if v.TypeString() == "v" {
					v = v.Variant()
				}
				if v.TypeString() == "i" {
					w.gameBadge.SetVisible(v.Int32() > 0)
				}
			}
		})
}

// refreshGameMode reads ClientCount once, off the GTK thread.
func (w *Window) refreshGameMode() {
	go func() {
		n, err := gameModeClients()
		glib.IdleAdd(func() { w.gameBadge.SetVisible(err == nil && n > 0) })
	}()
}
