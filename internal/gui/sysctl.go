// SPDX-License-Identifier: Apache-2.0

package gui

// sysctl.go — desktop and sysfs I/O for the Display and Power tabs: KScreen
// (rotation, refresh rate), KDE's ScreenBrightness and KWin's VirtualKeyboard
// over D-Bus, the amdgpu sensors and the battery.
//
// Nothing here touches a widget, so every function is safe to call from a
// goroutine — and every one that runs a command must be, since an exec can
// block. Parsing and decisions live in internal/system.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dahui/z13gui/internal/system"
)

// cmdTimeout bounds every external command. kscreen-doctor talks to the KScreen
// backend over D-Bus and can stall while KWin is reconfiguring outputs.
const cmdTimeout = 5 * time.Second

// runCmd runs name with args and returns trimmed stdout. A failure carries the
// command's stderr, which is what the error bar should show.
func runCmd(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return "", fmt.Errorf("%s: %s", name, firstLine(msg))
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return strings.TrimSpace(out.String()), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// desktopCaps records which desktop integrations exist in this session. Probed
// once at startup off the GTK thread; rows whose capability is missing are
// hidden rather than left to fail on every tap (gamescope, GNOME, a TTY).
type desktopCaps struct {
	kscreen    bool // kscreen-doctor present and answering
	brightness bool // org.kde.ScreenBrightness display0 answering
	osk        bool // KWin VirtualKeyboard available
}

// complete reports whether every integration has been found, i.e. there is
// nothing left worth probing for.
func (c desktopCaps) complete() bool {
	return c.kscreen && c.brightness && c.osk
}

// probeDesktopCaps checks for each integration not already in have. A found
// integration is never re-checked or dropped: the probe is for services that
// were not up yet (KDE's brightness and keyboard services can start after
// z13gui at login), not for health monitoring — a failing call reports itself.
func probeDesktopCaps(have desktopCaps) desktopCaps {
	c := have
	if !c.kscreen {
		if _, err := exec.LookPath("kscreen-doctor"); err == nil {
			_, err := readDisplay()
			c.kscreen = err == nil
		}
	}
	if _, err := exec.LookPath("busctl"); err == nil {
		if !c.brightness {
			_, _, err := readBrightness()
			c.brightness = err == nil
		}
		if !c.osk {
			v, err := busctlGet("org.kde.KWin", "/VirtualKeyboard", "org.kde.kwin.VirtualKeyboard", "available")
			c.osk = err == nil && v == "b true"
		}
	}
	return c
}

// --- KScreen ---

func readDisplay() (system.Display, error) {
	out, err := runCmd("kscreen-doctor", "-j")
	if err != nil {
		return system.Display{}, err
	}
	return system.ParseKscreenJSON([]byte(out))
}

// kscreenSet applies want to the panel and confirms it by reading the display
// back. kscreen-doctor exits 0 even when it cannot parse or ignores an
// argument (it prints "Unable to parse arguments" and carries on), so its exit
// status says nothing; the read-back is the check. Returns the display as read.
func kscreenSet(output string, want system.DisplayWant) (system.Display, error) {
	if output == "" {
		return system.Display{}, errors.New("no display found")
	}
	args := system.KscreenArgs(output, want)
	if len(args) == 0 {
		return readDisplay()
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "kscreen-doctor", args...).CombinedOutput()
	msg := firstLine(strings.TrimSpace(string(out)))
	if err != nil {
		return system.Display{}, fmt.Errorf("kscreen-doctor: %v %s", err, msg)
	}
	// KWin applies the configuration asynchronously; give it a moment.
	var d system.Display
	for i := 0; i < 5; i++ {
		if d, err = readDisplay(); err == nil && system.Applied(d, want) {
			return d, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return d, err
	}
	if msg == "" {
		msg = "the change did not take effect"
	}
	return d, fmt.Errorf("kscreen-doctor: %s", msg)
}

// --- D-Bus via busctl (session bus) ---

func busctlGet(dest, path, iface, prop string) (string, error) {
	return runCmd("busctl", "--user", "get-property", dest, path, iface, prop)
}

const (
	brightDest  = "org.kde.ScreenBrightness"
	brightPath  = "/org/kde/ScreenBrightness/display0"
	brightIface = "org.kde.ScreenBrightness.Display"
)

// readBrightness returns the internal panel's brightness and its maximum.
func readBrightness() (cur, max int, err error) {
	parse := func(prop string) (int, error) {
		v, err := busctlGet(brightDest, brightPath, brightIface, prop)
		if err != nil {
			return 0, err
		}
		// busctl prints "i 1000".
		_, num, _ := strings.Cut(v, " ")
		return strconv.Atoi(num)
	}
	if cur, err = parse("Brightness"); err != nil {
		return 0, 0, err
	}
	if max, err = parse("MaxBrightness"); err != nil {
		return 0, 0, err
	}
	if max <= 0 {
		return 0, 0, errors.New("display reports no brightness range")
	}
	return cur, max, nil
}

// setBrightness sets the raw brightness value. Flag 1 suppresses Plasma's
// on-screen indicator: the drawer's own slider already shows the level.
func setBrightness(value int) error {
	_, err := runCmd("busctl", "--user", "call", brightDest, brightPath, brightIface,
		"SetBrightness", "iu", strconv.Itoa(value), "1")
	return err
}

// toggleOnScreenKeyboard shows KWin's virtual keyboard, or hides it when it is
// already up. forceActivate is used rather than setting active=true because the
// latter only shows the keyboard while a text field has focus.
func toggleOnScreenKeyboard() error {
	const (
		dest  = "org.kde.KWin"
		path  = "/VirtualKeyboard"
		iface = "org.kde.kwin.VirtualKeyboard"
	)
	vis, err := busctlGet(dest, path, iface, "visible")
	if err != nil {
		return err
	}
	if vis == "b true" {
		_, err = runCmd("busctl", "--user", "set-property", dest, path, iface, "active", "b", "false")
		return err
	}
	_, err = runCmd("busctl", "--user", "call", dest, path, iface, "forceActivate")
	return err
}

// --- sysfs sensors ---

const (
	hwmonRoot    = "/sys/class/hwmon"
	powerSupply  = "/sys/class/power_supply"
	amdgpuHwmon  = "amdgpu"
	readFileSize = 4096
)

func readSmall(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, readFileSize)
	n, err := f.Read(buf)
	if n == 0 && err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

// findHwmon locates a hwmon directory by driver name.
func findHwmon(name string) string {
	dirs, _ := filepath.Glob(filepath.Join(hwmonRoot, "hwmon*"))
	names := make(map[string]string, len(dirs))
	for _, d := range dirs {
		if n, err := readSmall(filepath.Join(d, "name")); err == nil {
			names[d] = n
		}
	}
	return system.FindHwmon(names, name)
}

// sensors are the paths the power card reads every second. Resolved once:
// walking hwmon each tick is wasted work, and the paths do not move within a boot.
type sensors struct {
	apuPower string // amdgpu PPT, µW — package power on an APU
	gpuTemp  string // amdgpu edge temperature, m°C
	battery  string // power_supply dir of the first battery
	ac       string // power_supply dir of the mains adapter
}

func findSensors() sensors {
	var s sensors
	if d := findHwmon(amdgpuHwmon); d != "" {
		for _, f := range []string{"power1_input", "power1_average"} {
			if _, err := readSmall(filepath.Join(d, f)); err == nil {
				s.apuPower = filepath.Join(d, f)
				break
			}
		}
		s.gpuTemp = filepath.Join(d, "temp1_input")
	}
	supplies, _ := filepath.Glob(filepath.Join(powerSupply, "*"))
	for _, d := range supplies {
		t, _ := readSmall(filepath.Join(d, "type"))
		scope, _ := readSmall(filepath.Join(d, "scope"))
		if scope == "Device" { // peripherals (stylus, mouse) report batteries too
			continue
		}
		switch {
		case t == "Battery" && s.battery == "":
			s.battery = d
		case t == "Mains" && s.ac == "":
			s.ac = d
		}
	}
	return s
}

func (s sensors) readAPUWatts() (float64, bool) {
	if s.apuPower == "" {
		return 0, false
	}
	v, err := readSmall(s.apuPower)
	if err != nil {
		return 0, false
	}
	return system.ParseMicro(v)
}

func (s sensors) readGPUTemp() (int, bool) {
	if s.gpuTemp == "" {
		return 0, false
	}
	v, err := readSmall(s.gpuTemp)
	if err != nil {
		return 0, false
	}
	return system.ParseMilliC(v)
}

func (s sensors) readBattery() (system.Battery, bool) {
	if s.battery == "" {
		return system.Battery{}, false
	}
	v, err := readSmall(filepath.Join(s.battery, "uevent"))
	if err != nil {
		return system.Battery{}, false
	}
	return system.BatteryFromUevent(system.ParseUevent(v)), true
}

// acOnline reports mains status; known is false when no adapter was found.
func (s sensors) acOnline() (online, known bool) {
	if s.ac == "" {
		return false, false
	}
	v, err := readSmall(filepath.Join(s.ac, "online"))
	if err != nil {
		return false, false
	}
	return v == "1", true
}

// gameModeClients returns how many games Feral GameMode has registered;
// GameMode is active while it is above zero.
func gameModeClients() (int, error) {
	v, err := busctlGet("com.feralinteractive.GameMode", "/com/feralinteractive/GameMode",
		"com.feralinteractive.GameMode", "ClientCount")
	if err != nil {
		return 0, err
	}
	_, num, _ := strings.Cut(v, " ") // busctl prints "i 1"
	return strconv.Atoi(num)
}
