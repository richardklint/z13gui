// SPDX-License-Identifier: Apache-2.0

package system

import (
	"math"
	"sync"
)

// History is a fixed-size ring of samples, safe for one writer goroutine and
// readers on any other. The power sampler writes it once a second whether or
// not the drawer is open, so the graph has a minute of history the moment the
// drawer appears instead of starting empty.
type History struct {
	mu   sync.Mutex
	buf  []float64
	next int
	full bool
}

// NewHistory returns a History holding the last n samples (n >= 2).
func NewHistory(n int) *History {
	if n < 2 {
		n = 2
	}
	return &History{buf: make([]float64, n)}
}

// Add records a sample, overwriting the oldest once full.
func (h *History) Add(v float64) {
	h.mu.Lock()
	h.buf[h.next] = v
	h.next = (h.next + 1) % len(h.buf)
	if h.next == 0 {
		h.full = true
	}
	h.mu.Unlock()
}

// Snapshot returns the samples oldest first.
func (h *History) Snapshot() []float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.full {
		return append([]float64(nil), h.buf[:h.next]...)
	}
	out := make([]float64, 0, len(h.buf))
	out = append(out, h.buf[h.next:]...)
	return append(out, h.buf[:h.next]...)
}

// Cap is the number of samples the history holds when full.
func (h *History) Cap() int { return len(h.buf) }

// Point is a graph vertex in widget coordinates.
type Point struct{ X, Y float64 }

// GraphScale picks the y-axis maximum for samples: the next multiple of step
// above the largest sample, never below floor. Rounding keeps the axis from
// jittering every second as the peak moves by a fraction of a watt.
func GraphScale(samples []float64, floor, step float64) float64 {
	peak := floor
	for _, v := range samples {
		if v > peak {
			peak = v
		}
	}
	if step <= 0 || peak == floor {
		return peak
	}
	return math.Ceil(peak/step) * step
}

// GraphPoints maps samples onto a width×height box, right-aligned so the newest
// sample is always at the right edge and a partly filled history grows in from
// the right. capacity is the number of samples a full history spans.
func GraphPoints(samples []float64, capacity int, width, height, max float64) []Point {
	if len(samples) == 0 || capacity < 2 || max <= 0 {
		return nil
	}
	step := width / float64(capacity-1)
	offset := capacity - len(samples)
	pts := make([]Point, len(samples))
	for i, v := range samples {
		if v < 0 {
			v = 0
		}
		if v > max {
			v = max
		}
		pts[i] = Point{
			X: float64(offset+i) * step,
			Y: height - v/max*height,
		}
	}
	return pts
}
