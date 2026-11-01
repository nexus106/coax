// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package msi2500

import (
	"math"
	"math/cmplx"
	"math/rand/v2"
	"testing"
)

func TestFFTResolvesSignedTone(t *testing.T) {
	for _, n := range []int{8, 2048, 8192} {
		f, err := newFFT(n)
		if err != nil {
			t.Fatal(err)
		}
		x := make([]complex64, n)
		for i := range x {
			x[i] = complex64(cmplx.Exp(complex(0, -2*math.Pi*3*float64(i)/float64(n))))
		}
		f.transform(x)
		for k, v := range x {
			want := complex64(0)
			if k == n-3 {
				want = complex(float32(n), 0)
			}
			if cmplx.Abs(complex128(v-want)) > float64(n)*1e-4 {
				t.Fatalf("n=%d bin=%d got%v want%v", n, k, v, want)
			}
		}
	}
}

func TestAcquisitionFindsModeGuardAndFrequency(t *testing.T) {
	for _, mode := range []int{1, 2, 3} {
		for _, den := range []int{4, 8, 16, 32} {
			n := 1 << (10 + mode)
			cp := n / den
			r := rand.New(rand.NewPCG(12, 34))
			x := make([]complex64, 173)
			for sym := 0; sym < 16; sym++ {
				v := make([]complex64, n)
				for i := range v {
					v[i] = complex(float32(r.NormFloat64()), float32(r.NormFloat64()))
				}
				x = append(x, v[n-cp:]...)
				x = append(x, v...)
			}
			for i := range x {
				x[i] *= complex64(cmplx.Exp(complex(0, 2*math.Pi*.375*float64(i)/float64(n))))
			}
			got, ok := acquire(x)
			if !ok || got.Mode != mode || got.GuardDenominator != den || math.Abs(got.FractionalOffset-.375) > .01 {
				t.Fatalf("mode%d GI1/%d: %+v locked=%v", mode, den, got, ok)
			}
		}
	}
}

func TestAcquisitionRejectsNoise(t *testing.T) {
	r := rand.New(rand.NewPCG(55, 99))
	x := make([]complex64, 131072)
	for i := range x {
		x[i] = complex(float32(r.NormFloat64()), float32(r.NormFloat64()))
	}
	if s, ok := acquire(x); ok {
		t.Fatalf("noise acquired %+v", s)
	}
}

func TestTMCCRejectsDamagedParity(t *testing.T) {
	bits := tmccFixture()
	p, err := parseTMCC(bits, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !p.PartialReception || p.Layers[0].Constellation != 4 || p.Layers[0].Rate != 1 || p.Layers[0].Interleave != 4 || p.Layers[1].Constellation != 64 || p.Layers[1].Rate != 2 || p.Layers[1].Segments != 12 {
		t.Fatalf("parameters=%+v", p)
	}
	bits[43] ^= 1
	if _, err = parseTMCC(bits, 3); err == nil {
		t.Fatal("damaged TMCC accepted")
	}
}

// Fixture generated independently by polynomial long division of B31 §3.15.6.9.
func tmccFixture() []byte {
	s := "000110101111011100000011110100100101100010110100101100111111111111110010010110001011010010110011111111111111111111111111110010101111101000000110011100111110101110011100101101101110101000111110001010010110"
	out := make([]byte, len(s))
	for i := range s {
		out[i] = s[i] - '0'
	}
	return out
}
