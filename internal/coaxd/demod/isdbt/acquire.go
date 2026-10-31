// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package isdbt

import "math"

// acquisition uses the cyclic prefix repetition defined by STD-B31 §3.14.2.
// Folding over several symbols distinguishes the four possible guard lengths.
type acquisition struct {
	Mode, GuardDenominator, PrefixStart int
	FractionalOffset, Coherence         float64
}

func acquire(x []complex64) (acquisition, bool) {
	best := acquisition{}
	for mode := 1; mode <= 3; mode++ {
		n := 1 << (10 + mode)
		for _, den := range []int{4, 8, 16, 32} {
			cp := n / den
			period := n + cp
			if len(x) < period*6+n {
				continue
			}
			cross := make([]complex128, period)
			power := make([]float64, period)
			counts := make([]int, period)
			for i := 0; i < len(x)-n; i++ {
				a, b := x[i], x[i+n]
				k := i % period
				cross[k] += complex128(b * conjugate(a))
				power[k] += float64(real(a)*real(a)+imag(a)*imag(a)+real(b)*real(b)+imag(b)*imag(b)) * .5
				counts[k]++
			}
			for i := range cross {
				if counts[i] > 0 {
					cross[i] /= complex(float64(counts[i]), 0)
					power[i] /= float64(counts[i])
				}
			}
			var c complex128
			var p float64
			for i := 0; i < cp; i++ {
				c += cross[i]
				p += power[i]
			}
			for start := 0; start < period; start++ {
				if p > 1e-15 {
					score := math.Hypot(real(c), imag(c)) / p
					if score > best.Coherence {
						best = acquisition{mode, den, start, math.Atan2(imag(c), real(c)) / (2 * math.Pi), score}
					}
				}
				c += cross[(start+cp)%period] - cross[start]
				p += power[(start+cp)%period] - power[start]
			}
		}
	}
	return best, best.Coherence > .65
}
func conjugate(v complex64) complex64 { return complex(real(v), -imag(v)) }
