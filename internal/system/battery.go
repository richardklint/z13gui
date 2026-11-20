// SPDX-License-Identifier: Apache-2.0

package system

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Battery is a snapshot of /sys/class/power_supply/BAT*/uevent. Energy values
// are in µWh and power in µW, as the kernel reports them; a battery that
// reports charge (µAh) instead of energy is converted using the design voltage.
type Battery struct {
	Present      bool
	Status       string // "Charging", "Discharging", "Not charging", "Full", …
	Capacity     int    // percent
	EnergyNow    int64
	EnergyFull   int64
	EnergyDesign int64
	PowerNow     int64 // magnitude; the kernel's sign convention varies by driver
}

// ParseUevent parses a power_supply uevent file (POWER_SUPPLY_KEY=value lines).
func ParseUevent(data string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		out[strings.TrimPrefix(k, "POWER_SUPPLY_")] = v
	}
	return out
}

// BatteryFromUevent builds a Battery from a parsed uevent map.
func BatteryFromUevent(kv map[string]string) Battery {
	num := func(key string) int64 {
		n, err := strconv.ParseInt(kv[key], 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	b := Battery{
		Present:  kv["PRESENT"] == "1",
		Status:   kv["STATUS"],
		Capacity: int(num("CAPACITY")),
	}
	if _, ok := kv["ENERGY_NOW"]; ok {
		b.EnergyNow = num("ENERGY_NOW")
		b.EnergyFull = num("ENERGY_FULL")
		b.EnergyDesign = num("ENERGY_FULL_DESIGN")
		b.PowerNow = abs64(num("POWER_NOW"))
	} else if v := num("VOLTAGE_MIN_DESIGN"); v > 0 {
		// charge (µAh) × voltage (µV) / 1e6 = energy (µWh); current likewise.
		conv := func(x int64) int64 { return x * v / 1_000_000 }
		b.EnergyNow = conv(num("CHARGE_NOW"))
		b.EnergyFull = conv(num("CHARGE_FULL"))
		b.EnergyDesign = conv(num("CHARGE_FULL_DESIGN"))
		b.PowerNow = abs64(conv(num("CURRENT_NOW")))
	}
	return b
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// HealthPercent is full capacity as a share of design capacity, or -1 when
// either is unknown.
func (b Battery) HealthPercent() int {
	if b.EnergyFull <= 0 || b.EnergyDesign <= 0 {
		return -1
	}
	return int(math.Round(float64(b.EnergyFull) * 100 / float64(b.EnergyDesign)))
}

// Charging reports whether energy is flowing into the battery.
func (b Battery) Charging() bool { return b.Status == "Charging" }

// Discharging reports whether the battery is powering the machine.
func (b Battery) Discharging() bool { return b.Status == "Discharging" }

// RateWatts is the charge or discharge rate in watts.
func (b Battery) RateWatts() float64 { return float64(b.PowerNow) / 1e6 }

// RateLabel formats the rate with a direction sign: "+42 W", "−15.8 W", "0 W".
func (b Battery) RateLabel() string {
	w := b.RateWatts()
	switch {
	case w < 0.05:
		return "0 W"
	case b.Charging():
		return "+" + formatWatts(w)
	case b.Discharging():
		return "−" + formatWatts(w)
	}
	return formatWatts(w)
}

// formatWatts uses one decimal below 10 W, none above, so the readout keeps a
// stable width in a narrow column.
func formatWatts(w float64) string {
	if w < 10 {
		return fmt.Sprintf("%.1f W", w)
	}
	return fmt.Sprintf("%.0f W", w)
}

// FormatWatts is formatWatts without the unit, for the large power readout.
func FormatWatts(w float64) string {
	if w < 10 {
		return fmt.Sprintf("%.1f", w)
	}
	return fmt.Sprintf("%.0f", w)
}

// Minutes left until empty (discharging) or full (charging), or -1 when the
// rate is too small to estimate or the state has no end point.
func (b Battery) MinutesLeft() int {
	if b.PowerNow < 500_000 { // under 0.5 W: an estimate would be wild
		return -1
	}
	var energy int64
	switch {
	case b.Discharging():
		energy = b.EnergyNow
	case b.Charging():
		energy = b.EnergyFull - b.EnergyNow
	default:
		return -1
	}
	if energy <= 0 {
		return -1
	}
	return int(math.Round(float64(energy) / float64(b.PowerNow) * 60))
}

// FormatDuration renders minutes as "3 h 40 min" / "50 min".
func FormatDuration(min int) string {
	if min < 60 {
		return fmt.Sprintf("%d min", min)
	}
	return fmt.Sprintf("%d h %02d min", min/60, min%60)
}

// StatusLine is the battery card's subtitle, e.g. "Charging · full in 50 min".
// limitHeld means the charge limit is active and the battery sits at it, which
// the kernel reports as "Not charging".
func (b Battery) StatusLine(acOnline bool) string {
	left := b.MinutesLeft()
	switch {
	case b.Discharging():
		if left > 0 {
			return "On battery · " + FormatDuration(left) + " left"
		}
		return "On battery"
	case b.Charging():
		if left > 0 {
			return "Charging · full in " + FormatDuration(left)
		}
		return "Charging"
	case b.Status == "Full":
		return "Plugged in · full"
	case acOnline:
		return "Plugged in · held at limit"
	}
	return b.Status
}
