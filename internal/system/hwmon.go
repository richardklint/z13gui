// SPDX-License-Identifier: Apache-2.0

package system

import (
	"sort"
	"strconv"
	"strings"
)

// FindHwmon returns the hwmon directory whose name file reads want, given a
// map of directory → name contents. hwmonN numbering is not stable across
// boots, so sensors are found by driver name. When several match, the lowest
// numbered wins, which keeps the choice stable within a boot.
func FindHwmon(names map[string]string, want string) string {
	var hits []string
	for dir, name := range names {
		if strings.TrimSpace(name) == want {
			hits = append(hits, dir)
		}
	}
	if len(hits) == 0 {
		return ""
	}
	sort.Strings(hits)
	return hits[0]
}

// ParseMicro parses a sysfs integer in micro-units (µW, µWh) and returns it
// divided by 1e6. ok is false for anything unparseable.
func ParseMicro(s string) (float64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(n) / 1e6, true
}

// ParseMilliC parses a hwmon temperature (millidegrees Celsius) to whole degrees.
func ParseMilliC(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return (n + 500) / 1000, true
}
