// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package msi2500

import "math"

// samplingClock measures input samples per nominal OFDM sample. Regressing
// tracked cyclic-prefix positions averages integer timing noise without
// mistaking an individual one-sample timing correction for a rate change.
type samplingClock struct {
	residual        [512]float64
	at, count       int
	origin, symbols int64
	rate            float64
}

func (c *samplingClock) observe(position int64, period int) {
	if c.count == 0 {
		c.origin = position
	}
	c.residual[c.at] = float64(position - c.origin - c.symbols*int64(period))
	c.symbols++
	c.at = (c.at + 1) % len(c.residual)
	c.count = min(c.count+1, len(c.residual))
	if c.count < 64 {
		return
	}
	first := 0
	if c.count == len(c.residual) {
		first = c.at
	}
	center := float64(c.count-1) * .5
	var slope float64
	for i := 0; i < c.count; i++ {
		slope += (float64(i) - center) * c.residual[(first+i)%len(c.residual)]
	}
	n := float64(c.count)
	rate := slope / (n * (n*n - 1) / 12) / float64(period)
	// Crystal drift is small. Keep bad timing estimates from leaving the guard.
	rate = math.Max(-200e-6, math.Min(200e-6, rate))
	c.rate += .05 * (rate - c.rate)
}

// A 16-tap Blackman-windowed sinc retains the full ISDB-T occupied band.
// Quantizing fractional delay to 1/1024 sample avoids trigonometry in the
// sample loop; the table is shared and never modified after initialization.
var fractionalTaps = func() [1025][16]float32 {
	var table [1025][16]float32
	for phase := range table {
		fraction := float64(phase) / 1024
		var sum float64
		var taps [16]float64
		for j := range taps {
			d := float64(j-7) - fraction
			sinc := 1.0
			if d != 0 {
				sinc = math.Sin(math.Pi*d) / (math.Pi * d)
			}
			window := .42 + .5*math.Cos(math.Pi*d/8) + .08*math.Cos(2*math.Pi*d/8)
			taps[j] = sinc * window
			sum += taps[j]
		}
		for j, v := range taps {
			table[phase][j] = float32(v / sum)
		}
	}
	return table
}()

func fractionalSample(x []complex64, position float64) complex64 {
	center := int(math.Floor(position))
	phase := int((position-float64(center))*1024 + .5)
	taps := &fractionalTaps[phase]
	var out complex64
	for j, tap := range taps {
		out += x[center+j-7] * complex(tap, 0)
	}
	return out
}
