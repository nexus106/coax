// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package msi2500

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestSamplingClockTracksSignedRateAndReset(t *testing.T) {
	for _, period := range []int{2112, 2304, 9216} {
		for _, ppm := range []float64{-100, -25, 0, 25, 100} {
			var clock samplingClock
			for i := 0; i < 2048; i++ {
				// Deterministic prefix jitter is independent of the known clock rate.
				jitter := int64((i*17)%7 - 3)
				position := int64(9000000000) + int64(math.Round(float64(i*period)*(1+ppm*1e-6))) + jitter
				clock.observe(position, period)
			}
			if math.Abs(clock.rate*1e6-ppm) > .4 {
				t.Fatalf("period=%d ppm=%g estimated=%g", period, ppm, clock.rate*1e6)
			}
			sync := NewSynchronizer()
			sync.clock = clock
			sync.Reset()
			if sync.clock.rate != 0 || sync.clock.count != 0 {
				t.Fatal("retained stale clock")
			}
		}
	}
}

func TestFractionalSamplePreservesWidebandTones(t *testing.T) {
	for _, freq := range []float64{-.34, -.2, 0, .2, .34} {
		x := make([]complex64, 160)
		for i := range x {
			x[i] = complex64(cmplx.Exp(complex(0, 2*math.Pi*freq*float64(i))))
		}
		for _, mu := range []float64{0, .07, .25, .5, .91, .9999} {
			at := 64 + mu
			got := fractionalSample(x, at)
			want := cmplx.Exp(complex(0, 2*math.Pi*freq*at))
			if err := cmplx.Abs(complex128(got) - want); err > .003 {
				t.Fatalf("frequency=%g fraction=%g error=%g", freq, mu, err)
			}
		}
	}
}

func TestTransformCorrectsSamplingClockAcrossOccupiedBand(t *testing.T) {
	const n = 8192
	const start = 64
	for _, rate := range []float64{-200e-6, -25e-6, 0, 25e-6, 200e-6} {
		for _, offset := range []float64{0, 12.375} {
			s := NewSynchronizer()
			s.n = n
			s.offset = offset
			s.total = 9999999
			s.clock.rate = rate
			s.plan, _ = newFFT(n)
			s.buffer = make([]complex64, n+128)
			s.spectrum = make([]complex64, n)
			bins := []int{-2800, -900, 700, 2800}
			for i := range s.buffer {
				var v complex128
				for _, k := range bins {
					v += cmplx.Exp(complex(0, 2*math.Pi*float64(k*i)/(n*(1+rate))))
				}
				v *= cmplx.Exp(complex(0, 2*math.Pi*offset*float64(s.total+int64(i))/n))
				s.buffer[i] = complex64(v)
			}
			s.transform(start)
			var errorPower float64
			for bin, v := range s.spectrum {
				var want complex128
				for _, k := range bins {
					if bin == (k+n)%n {
						want = n * cmplx.Exp(complex(0, 2*math.Pi*float64(k)*(start-n*.5*rate)/(n*(1+rate))))
					}
				}
				e := cmplx.Abs(complex128(v) - want)
				errorPower += e * e
			}
			if evm := math.Sqrt(errorPower / (float64(len(bins)) * n * n)); evm > .005 {
				t.Fatalf("rate=%g offset=%g EVM=%g", rate, offset, evm)
			}
		}
	}
}
