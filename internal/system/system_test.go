// SPDX-License-Identifier: Apache-2.0

package system

import (
	"math"
	"reflect"
	"testing"
)

// A trimmed `kscreen-doctor -j` from a GZ302EA: the panel offers several rates
// near 60 and 180 at native resolution, plus lower resolutions that must be
// ignored, and an external monitor that is not the panel.
const kscreenFixture = `{"outputs":[
 {"name":"DP-2","connected":true,"enabled":true,"type":14,"rotation":1,"autoRotatePolicy":0,
  "currentModeId":"x1","modes":[{"id":"x1","refreshRate":144,"size":{"width":2560,"height":1440}}]},
 {"name":"eDP-1","connected":true,"enabled":true,"type":7,"rotation":1,"autoRotatePolicy":1,
  "currentModeId":"1","modes":[
   {"id":"1","refreshRate":180,"size":{"width":2560,"height":1600}},
   {"id":"2","refreshRate":179.92,"size":{"width":2560,"height":1600}},
   {"id":"3","refreshRate":59.81,"size":{"width":2560,"height":1600}},
   {"id":"4","refreshRate":60.0,"size":{"width":2560,"height":1600}},
   {"id":"5","refreshRate":59.9,"size":{"width":2560,"height":1600}},
   {"id":"10","refreshRate":180,"size":{"width":1280,"height":720}},
   {"id":"11","refreshRate":120,"size":{"width":1280,"height":720}}]}]}`

func TestParseKscreenJSON_PicksPanel(t *testing.T) {
	d, err := ParseKscreenJSON([]byte(kscreenFixture))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "eDP-1" {
		t.Errorf("Name = %q, want eDP-1 (the panel, not the first output)", d.Name)
	}
	if d.Rotation != RotationNormal || d.Policy != PolicyInTabletMode {
		t.Errorf("rotation/policy = %d/%d", d.Rotation, d.Policy)
	}
	if d.CurrentHz != 180 {
		t.Errorf("CurrentHz = %d, want 180", d.CurrentHz)
	}
	want := []Rate{{Hz: 60, ModeID: "4"}, {Hz: 180, ModeID: "1"}}
	if !reflect.DeepEqual(d.Rates, want) {
		t.Errorf("Rates = %+v, want %+v (other resolutions excluded, 60.00 preferred)", d.Rates, want)
	}
}

func TestParseKscreenJSON_PrefersCurrentModeInGroup(t *testing.T) {
	// The current mode is 179.92: selecting "180 Hz" must not switch away from it.
	data := []byte(`{"outputs":[{"name":"eDP-1","connected":true,"enabled":true,"type":7,
	 "currentModeId":"2","modes":[
	 {"id":"1","refreshRate":180,"size":{"width":10,"height":10}},
	 {"id":"2","refreshRate":179.92,"size":{"width":10,"height":10}}]}]}`)
	d, err := ParseKscreenJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rates) != 1 || d.Rates[0].ModeID != "2" {
		t.Errorf("Rates = %+v, want the current mode 2", d.Rates)
	}
}

func TestParseKscreenJSON_Errors(t *testing.T) {
	if _, err := ParseKscreenJSON([]byte("not json")); err == nil {
		t.Error("want error for invalid JSON")
	}
	if _, err := ParseKscreenJSON([]byte(`{"outputs":[{"name":"x","connected":false}]}`)); err != ErrNoDisplay {
		t.Errorf("err = %v, want ErrNoDisplay", err)
	}
}

func TestQuickRates(t *testing.T) {
	r := []Rate{{Hz: 48}, {Hz: 60}, {Hz: 120}, {Hz: 180}}
	got := QuickRates(r)
	if len(got) != 2 || got[0].Hz != 48 || got[1].Hz != 180 {
		t.Errorf("QuickRates = %+v", got)
	}
	if len(QuickRates(r[:2])) != 2 {
		t.Error("two rates should pass through")
	}
}

func TestRotationAndPolicyArgs(t *testing.T) {
	for r, want := range map[int]string{1: "normal", 2: "left", 4: "inverted", 8: "right", 3: ""} {
		if got := RotationArg(r); got != want {
			t.Errorf("RotationArg(%d) = %q, want %q", r, got, want)
		}
	}
	for _, p := range []int{PolicyNever, PolicyInTabletMode, PolicyAlways} {
		if PolicyFromArg(PolicyArg(p)) != p {
			t.Errorf("policy %d does not round-trip", p)
		}
	}
	if PolicyFromArg("bogus") != PolicyInTabletMode {
		t.Error("unknown policy should default to in-tablet-mode")
	}
}

// Real readings from the GZ302EA at an 80% limit on AC.
const ueventFixture = `POWER_SUPPLY_NAME=BAT0
POWER_SUPPLY_TYPE=Battery
POWER_SUPPLY_STATUS=Not charging
POWER_SUPPLY_PRESENT=1
POWER_SUPPLY_POWER_NOW=0
POWER_SUPPLY_ENERGY_FULL_DESIGN=70003000
POWER_SUPPLY_ENERGY_FULL=63018000
POWER_SUPPLY_ENERGY_NOW=50414000
POWER_SUPPLY_CAPACITY=80
`

func TestBatteryFromUevent(t *testing.T) {
	b := BatteryFromUevent(ParseUevent(ueventFixture))
	if !b.Present || b.Capacity != 80 || b.Status != "Not charging" {
		t.Fatalf("parsed %+v", b)
	}
	if h := b.HealthPercent(); h != 90 {
		t.Errorf("HealthPercent = %d, want 90", h)
	}
	if b.RateLabel() != "0 W" {
		t.Errorf("RateLabel = %q", b.RateLabel())
	}
	if got := b.StatusLine(true); got != "Plugged in · held at limit" {
		t.Errorf("StatusLine = %q", got)
	}
	if b.MinutesLeft() != -1 {
		t.Error("no estimate expected at 0 W")
	}
}

func TestBatteryChargeUnits(t *testing.T) {
	// Charge-reporting batteries (µAh/µA) are converted via design voltage.
	b := BatteryFromUevent(ParseUevent(`POWER_SUPPLY_STATUS=Discharging
POWER_SUPPLY_VOLTAGE_MIN_DESIGN=15000000
POWER_SUPPLY_CHARGE_NOW=2000000
POWER_SUPPLY_CHARGE_FULL=4000000
POWER_SUPPLY_CHARGE_FULL_DESIGN=5000000
POWER_SUPPLY_CURRENT_NOW=-1000000`))
	if b.EnergyNow != 30_000_000 || b.PowerNow != 15_000_000 {
		t.Fatalf("converted %+v", b)
	}
	if b.HealthPercent() != 80 {
		t.Errorf("HealthPercent = %d", b.HealthPercent())
	}
	if b.MinutesLeft() != 120 {
		t.Errorf("MinutesLeft = %d, want 120", b.MinutesLeft())
	}
	if b.RateLabel() != "−15 W" {
		t.Errorf("RateLabel = %q", b.RateLabel())
	}
	if got := b.StatusLine(false); got != "On battery · 2 h 00 min left" {
		t.Errorf("StatusLine = %q", got)
	}
}

func TestBatteryCharging(t *testing.T) {
	b := Battery{Status: "Charging", EnergyNow: 50e6, EnergyFull: 63e6, PowerNow: 42e6}
	if b.RateLabel() != "+42 W" {
		t.Errorf("RateLabel = %q", b.RateLabel())
	}
	if got := b.StatusLine(true); got != "Charging · full in 19 min" {
		t.Errorf("StatusLine = %q", got)
	}
	if (Battery{}).HealthPercent() != -1 {
		t.Error("unknown health should be -1")
	}
}

func TestFormatWatts(t *testing.T) {
	if FormatWatts(7.25) != "7.2" && FormatWatts(7.25) != "7.3" {
		t.Errorf("FormatWatts(7.25) = %q", FormatWatts(7.25))
	}
	if FormatWatts(27.42) != "27" {
		t.Errorf("FormatWatts(27.42) = %q", FormatWatts(27.42))
	}
}

func TestHistory(t *testing.T) {
	h := NewHistory(3)
	if len(h.Snapshot()) != 0 {
		t.Fatal("new history should be empty")
	}
	h.Add(1)
	h.Add(2)
	if got := h.Snapshot(); !reflect.DeepEqual(got, []float64{1, 2}) {
		t.Errorf("partial = %v", got)
	}
	h.Add(3)
	h.Add(4)
	if got := h.Snapshot(); !reflect.DeepEqual(got, []float64{2, 3, 4}) {
		t.Errorf("wrapped = %v, want oldest first", got)
	}
	if NewHistory(0).Cap() != 2 {
		t.Error("capacity floor is 2")
	}
}

func TestGraphScale(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{nil, 30},                 // floor when empty
		{[]float64{12, 18}, 30},   // below floor
		{[]float64{31}, 40},       // next step up
		{[]float64{56.2, 40}, 60}, // gaming
		{[]float64{60}, 60},       // exactly on a step
	}
	for _, c := range cases {
		if got := GraphScale(c.in, 30, 10); got != c.want {
			t.Errorf("GraphScale(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestGraphPoints(t *testing.T) {
	pts := GraphPoints([]float64{0, 50, 100}, 5, 100, 50, 100)
	// Three samples in a five-wide history start at slot 2 (right-aligned).
	want := []Point{{50, 50}, {75, 25}, {100, 0}}
	for i := range want {
		if math.Abs(pts[i].X-want[i].X) > 1e-9 || math.Abs(pts[i].Y-want[i].Y) > 1e-9 {
			t.Errorf("pt %d = %+v, want %+v", i, pts[i], want[i])
		}
	}
	if clamp := GraphPoints([]float64{-5, 500}, 2, 10, 10, 100); clamp[0].Y != 10 || clamp[1].Y != 0 {
		t.Errorf("values should clamp to the box: %+v", clamp)
	}
	if GraphPoints(nil, 5, 1, 1, 1) != nil || GraphPoints([]float64{1}, 5, 1, 1, 0) != nil {
		t.Error("degenerate input should yield nil")
	}
}

func TestChargeDecisions(t *testing.T) {
	if !LimitActive(80) || LimitActive(100) || LimitActive(0) {
		t.Error("LimitActive")
	}
	if PreferredLimit(60) != 60 || PreferredLimit(100) != DefaultLimit || PreferredLimit(0) != DefaultLimit {
		t.Error("PreferredLimit")
	}
	cases := []struct {
		pending, known, online, want bool
	}{
		{true, true, false, true},
		{true, true, true, false},   // still charging
		{false, true, false, false}, // nothing pending
		{true, false, false, false}, // AC unreadable: keep the user's choice
	}
	for _, c := range cases {
		if got := ShouldRestore(c.pending, c.known, c.online); got != c.want {
			t.Errorf("ShouldRestore(%v,%v,%v) = %v", c.pending, c.known, c.online, got)
		}
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := LoadPrefs(); got != DefaultPrefs() {
		t.Errorf("missing file = %+v, want defaults", got)
	}
	p := Prefs{ChargeLimit: 60, RestoreOnUnplug: false, PendingRestore: true, AutoRotate: "always", Tab: "display"}
	SavePrefs(p)
	if got := LoadPrefs(); got != p {
		t.Errorf("round trip = %+v, want %+v", got, p)
	}
}

func TestParsePrefs_RejectsBadValues(t *testing.T) {
	p := ParsePrefs("charge_limit = 100\nauto_rotate = sideways\ntab = nope\nrestore_on_unplug = maybe\n# c\njunk")
	if p != DefaultPrefs() {
		t.Errorf("bad values should keep defaults, got %+v", p)
	}
}

func TestFindHwmon(t *testing.T) {
	names := map[string]string{"/h/hwmon6": "k10temp\n", "/h/hwmon5": "amdgpu\n", "/h/hwmon9": "amdgpu"}
	if got := FindHwmon(names, "amdgpu"); got != "/h/hwmon5" {
		t.Errorf("FindHwmon = %q", got)
	}
	if FindHwmon(names, "nvme") != "" {
		t.Error("missing sensor should be empty")
	}
}

func TestParseSensors(t *testing.T) {
	if w, ok := ParseMicro("27420000\n"); !ok || w != 27.42 {
		t.Errorf("ParseMicro = %v %v", w, ok)
	}
	if _, ok := ParseMicro("x"); ok {
		t.Error("ParseMicro should reject junk")
	}
	if c, ok := ParseMilliC("57750"); !ok || c != 58 {
		t.Errorf("ParseMilliC = %v %v", c, ok)
	}
}
