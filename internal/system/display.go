// SPDX-License-Identifier: Apache-2.0

// Package system holds the decisions behind the drawer's desktop-integration
// controls — display rotation and refresh rate, battery readouts, the power
// history graph and the charge-limit top-up — kept out of internal/gui so they
// can be tested without CGO or GTK headers.
//
// The GTK side only runs the commands and reads the files; every parse,
// selection and calculation lives here.
package system

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
)

// Rotation values as kscreen-doctor reports them (KScreen::Output::Rotation).
const (
	RotationNormal   = 1
	RotationLeft     = 2
	RotationInverted = 4
	RotationRight    = 8
)

// Auto-rotate policies as kscreen-doctor reports them
// (KScreen::Output::AutoRotatePolicy).
const (
	PolicyNever        = 0
	PolicyInTabletMode = 1
	PolicyAlways       = 2
)

// RotationArg returns the kscreen-doctor argument for a rotation value
// ("output.<name>.rotation.<arg>"), or "" for an unknown value. Upright is
// "none" — kscreen-doctor silently ignores "normal".
func RotationArg(r int) string {
	switch r {
	case RotationNormal:
		return "none"
	case RotationLeft:
		return "left"
	case RotationInverted:
		return "inverted"
	case RotationRight:
		return "right"
	}
	return ""
}

// PolicyArg returns the kscreen-doctor argument for an auto-rotate policy
// ("output.<name>.autoRotatePolicy.<arg>"), or "" for an unknown value. The
// spellings are kscreen-doctor's own and are not in its --help.
func PolicyArg(p int) string {
	switch p {
	case PolicyNever:
		return "never"
	case PolicyInTabletMode:
		return "inTabletMode"
	case PolicyAlways:
		return "always"
	}
	return ""
}

// PolicyFromArg is the inverse of PolicyArg. Unknown strings (including the
// "in-tablet-mode" spelling earlier builds saved) return PolicyInTabletMode,
// the Plasma default for a convertible.
func PolicyFromArg(s string) int {
	switch s {
	case "never":
		return PolicyNever
	case "always":
		return PolicyAlways
	}
	return PolicyInTabletMode
}

// Rate is one selectable refresh rate at the output's current resolution.
type Rate struct {
	Hz     int    // rounded, for display ("60 Hz")
	ModeID string // kscreen mode id to select it
}

// Display is the internal panel as kscreen-doctor describes it.
type Display struct {
	Name          string // e.g. "eDP-1"
	Rotation      int
	Policy        int
	CurrentModeID string
	CurrentHz     int    // rounded refresh rate of the current mode
	Rates         []Rate // distinct rates at the current resolution, ascending
}

// ErrNoDisplay means the JSON parsed but held no connected, enabled output.
var ErrNoDisplay = errors.New("no connected display")

type kscreenSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type kscreenMode struct {
	ID          string      `json:"id"`
	RefreshRate float64     `json:"refreshRate"`
	Size        kscreenSize `json:"size"`
}

type kscreenOutput struct {
	Name             string        `json:"name"`
	Connected        bool          `json:"connected"`
	Enabled          bool          `json:"enabled"`
	Type             int           `json:"type"`
	Rotation         int           `json:"rotation"`
	AutoRotatePolicy int           `json:"autoRotatePolicy"`
	CurrentModeID    string        `json:"currentModeId"`
	Modes            []kscreenMode `json:"modes"`
}

// kscreenTypePanel is KScreen::Output::Type::Panel — a laptop's built-in screen.
const kscreenTypePanel = 7

// ParseKscreenJSON reads `kscreen-doctor -j` output and returns the internal
// panel, falling back to the first connected, enabled output when no output
// reports itself as a panel.
func ParseKscreenJSON(data []byte) (Display, error) {
	var doc struct {
		Outputs []kscreenOutput `json:"outputs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Display{}, err
	}
	var pick *kscreenOutput
	for i := range doc.Outputs {
		o := &doc.Outputs[i]
		if !o.Connected || !o.Enabled {
			continue
		}
		if o.Type == kscreenTypePanel {
			pick = o
			break
		}
		if pick == nil {
			pick = o
		}
	}
	if pick == nil {
		return Display{}, ErrNoDisplay
	}

	d := Display{
		Name:          pick.Name,
		Rotation:      pick.Rotation,
		Policy:        pick.AutoRotatePolicy,
		CurrentModeID: pick.CurrentModeID,
	}
	var cur *kscreenMode
	for i := range pick.Modes {
		if pick.Modes[i].ID == pick.CurrentModeID {
			cur = &pick.Modes[i]
			break
		}
	}
	if cur == nil {
		return d, nil
	}
	d.CurrentHz = int(math.Round(cur.RefreshRate))
	d.Rates = ratesAt(pick.Modes, cur.Size, pick.CurrentModeID)
	return d, nil
}

// ratesAt groups the modes at size by rounded refresh rate. Panels advertise
// several near-identical rates (59.81, 59.85, … 60.00); for each group it keeps
// the current mode when the group contains it, otherwise the mode closest to the
// whole number, which is the one the user means by "60 Hz".
func ratesAt(modes []kscreenMode, size kscreenSize, currentID string) []Rate {
	type best struct {
		id   string
		dist float64
		cur  bool
	}
	groups := map[int]best{}
	for _, m := range modes {
		if m.Size != size || m.RefreshRate <= 0 {
			continue
		}
		hz := int(math.Round(m.RefreshRate))
		dist := math.Abs(m.RefreshRate - float64(hz))
		isCur := m.ID == currentID
		b, ok := groups[hz]
		if !ok || (isCur && !b.cur) || (!b.cur && dist < b.dist) {
			groups[hz] = best{id: m.ID, dist: dist, cur: isCur}
		}
	}
	rates := make([]Rate, 0, len(groups))
	for hz, b := range groups {
		rates = append(rates, Rate{Hz: hz, ModeID: b.id})
	}
	sort.Slice(rates, func(i, j int) bool { return rates[i].Hz < rates[j].Hz })
	return rates
}

// QuickRates picks the rates the drawer offers as buttons: the lowest and the
// highest. A panel exposing many intermediate rates would otherwise not fit, and
// the use case is "save battery" versus "smooth". Returns the input unchanged
// when it has two or fewer entries.
func QuickRates(rates []Rate) []Rate {
	if len(rates) <= 2 {
		return rates
	}
	return []Rate{rates[0], rates[len(rates)-1]}
}

// DisplayWant is a change to the panel. Zero fields are left alone: Policy and
// Rotation use -1/0 as "unchanged" because 0 is a real policy (never).
type DisplayWant struct {
	Policy   int    // -1 = unchanged
	Rotation int    // 0 = unchanged
	ModeID   string // "" = unchanged
}

// KeepPolicy is DisplayWant.Policy's "leave it alone" value.
const KeepPolicy = -1

// KscreenArgs builds the kscreen-doctor arguments for want, applied in one
// call so a rotate-and-lock is a single configuration change.
func KscreenArgs(output string, want DisplayWant) []string {
	var args []string
	prefix := "output." + output + "."
	if a := PolicyArg(want.Policy); want.Policy != KeepPolicy && a != "" {
		args = append(args, prefix+"autoRotatePolicy."+a)
	}
	if a := RotationArg(want.Rotation); a != "" {
		args = append(args, prefix+"rotation."+a)
	}
	if want.ModeID != "" {
		args = append(args, prefix+"mode."+want.ModeID)
	}
	return args
}

// Applied reports whether d reflects want. kscreen-doctor exits 0 when it
// rejects or ignores an argument, so reading the display back is the only way
// to know a change took effect.
func Applied(d Display, want DisplayWant) bool {
	if want.Policy != KeepPolicy && d.Policy != want.Policy {
		return false
	}
	if want.Rotation != 0 && d.Rotation != want.Rotation {
		return false
	}
	if want.ModeID != "" && d.CurrentModeID != want.ModeID {
		return false
	}
	return true
}
