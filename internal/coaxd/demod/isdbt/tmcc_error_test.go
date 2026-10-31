// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package isdbt

import (
	"errors"
	"math"
	"math/cmplx"
	"testing"
)

func TestUnsupportedTMCCFollowedBySignalLossReportsError(t *testing.T) {
	const mode = 1
	const n = 2048
	const cp = 256
	f, _ := newFFT(n)
	pilots := pilotSigns(1405)
	bits := tmccFixture()
	bits[28] = 0
	bits[29] = 0
	bits[30] = 0
	work := make([]byte, 184)
	copy(work, bits[20:122])
	for i := 0; i < 102; i++ {
		if work[i] != 0 {
			for _, power := range []int{82, 77, 76, 71, 67, 66, 56, 52, 48, 40, 36, 34, 24, 22, 18, 10, 4, 0} {
				work[i+82-power] ^= 1
			}
		}
	}
	copy(bits[122:], work[102:])
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
	wave = append(wave, make([]complex64, 32768)...)
	d, _ := New(Config{})
	_, err := d.PushIQ(wave, false)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported TMCC followed by signal loss returned %v", err)
	}
	if _, err := d.PushIQ(make([]complex64, 131072), false); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error was not latched: %v", err)
	}
	if _, err := d.PushIQ(nil, true); err != nil {
		t.Fatalf("explicit reset retained error: %v", err)
	}
}
