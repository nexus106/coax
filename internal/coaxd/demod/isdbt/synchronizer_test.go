// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package isdbt

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestSynchronizerFindsStandardTMCCWithFrequencyOffset(t *testing.T) {
	const mode = 1
	const n = 2048
	const cp = 256
	f, _ := newFFT(n)
	pilots := pilotSigns(1405)
	bits := tmccFixture()
	wave := make([]complex64, 0, (n+cp)*440)
	dbpsk := float32(1)
	for s := 0; s < 440; s++ {
		frame := s % 204
		if frame == 0 {
			dbpsk = 1
		} else if bits[frame] != 0 {
			dbpsk = -dbpsk
		}
		bins := make([]complex64, n)
		for k := 0; k < 1405; k++ {
			bins[(n/2-702+k+n/2)%n] = complex(.707, .707)
		}
		for k := 3 * (s % 4); k < 1405; k += 12 {
			bins[(n/2-702+k+n/2)%n] = complex(pilots[k]*4/3, 0)
		}
		bins[(n/2-702+1404+n/2)%n] = complex(pilots[1404]*4/3, 0)
		for _, row := range tmccRows[mode] {
			for seg, k := range row {
				idx := 108*seg + k
				bins[(n/2-702+idx+n/2)%n] = complex(pilots[idx]*dbpsk*4/3, 0)
			}
		}
		for i := range bins {
			bins[i] = conjugate(bins[i])
		}
		f.transform(bins)
		for i := range bins {
			bins[i] = conjugate(bins[i]) / n
		}
		wave = append(wave, bins[n-cp:]...)
		wave = append(wave, bins...)
	}
	for i := range wave {
		wave[i] *= complex64(cmplx.Exp(complex(0, 2*math.Pi*12.375*float64(i)/n)))
	}
	sync := NewSynchronizer()
	frames := 0
	for start := 0; start < len(wave); start += 32768 {
		end := min(start+32768, len(wave))
		sync.PushIQ(wave[start:end], false, func(s Symbol) {
			if s.Parameters != nil {
				frames++
			}
		})
	}
	if frames == 0 || sync.Snapshot().Parameters == nil {
		t.Fatalf("TMCC not found: %+v", sync.Snapshot())
	}
	sync.PushIQ(nil, true, nil)
	if sync.Snapshot().Parameters != nil {
		t.Fatal("sample loss retained stale parameters")
	}
}
