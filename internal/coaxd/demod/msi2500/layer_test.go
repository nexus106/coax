// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package msi2500

import "testing"

func TestFrequencyPermutation(t *testing.T) {
	for mode := 1; mode <= 3; mode++ {
		p := frequencyPermutation(mode, true)
		seen := make([]bool, len(p))
		for _, v := range p {
			if v < 0 || v >= len(p) || seen[v] {
				t.Fatal(mode, v)
			}
			seen[v] = true
		}
		if p[0] != int(carrierRandomizing[mode-1][0]) {
			t.Fatal("partial segment must remain isolated")
		}
	}
}
func TestDemapKnown(t *testing.T) {
	for _, tc := range []struct {
		q    int
		c    complex64
		want byte
	}{{4, complex(1, 1), 0}, {4, complex(-1, -1), 3}, {16, complex(0.31622777, 0.31622777), 3}, {64, complex(0.15430335, 0.15430335), 12}} {
		b := demap(nil, []complex64{tc.c}, tc.q)
		var got byte
		for _, v := range b {
			got = got*2 + v
		}
		if got != tc.want {
			t.Fatalf("%d %v = %d want %d", tc.q, tc.c, got, tc.want)
		}
	}
}

func TestEnergyDispersalStandardRegister(t *testing.T) {
	r := uint16(0xa9)
	want := []byte{0x03, 0xf6, 0x08, 0x34, 0x30, 0xb8, 0xa3, 0x93}
	for _, w := range want {
		if got := nextEnergyByte(&r); got != w {
			t.Fatalf("got%x want%x", got, w)
		}
	}
}
func TestTimeDelayFixture(t *testing.T) {
	d := newTimeDeinterleaver(96, 1)
	in := make([]complex64, 96)
	out := make([]complex64, 96)
	for symbol := 0; symbol < 96; symbol++ {
		clear(in)
		if symbol == 0 {
			for i := range in {
				in[i] = 1
			}
		}
		d.push(out, in)
		for i, v := range out {
			want := complex64(0)
			if symbol == 95-(i*5)%96 {
				want = 1
			}
			if v != want {
				t.Fatalf("symbol%d carrier%d got%v want%v", symbol, i, v, want)
			}
		}
	}
}
func TestBitDelayFixture(t *testing.T) {
	for _, width := range []int{2, 4, 6} {
		d := newBitDeinterleaver(width)
		for symbol := 0; symbol <= 120; symbol++ {
			in := make([]byte, width)
			if symbol == 0 {
				for i := range in {
					in[i] = 1
				}
			}
			out := d.push(nil, in)
			for i, v := range out {
				want := byte(0)
				if symbol == 120-i*120/(width-1) {
					want = 1
				}
				if v != want {
					t.Fatalf("width%d symbol%d bit%d", width, symbol, i)
				}
			}
		}
	}
}
func TestDecoderLossReset(t *testing.T) {
	d, _ := New(Config{})
	p := Parameters{}
	d.p = &p
	d.layers[0] = &layerDecoder{packets: 5}
	if !d.Locked() {
		t.Fatal("valid packet history must lock")
	}
	d.layers[0].consecutiveFailures = 16
	if d.Locked() {
		t.Fatal("uncorrectable packet run must drop lock")
	}
	d.PushIQ(nil, true)
	if d.Locked() || d.Snapshot().TMCCFrames != 0 {
		t.Fatal("discontinuity retained lock")
	}
}

func TestDecoderInternalLossResetsLayers(t *testing.T) {
	d, _ := New(Config{})
	p := Parameters{Mode: 3, GuardDenominator: 8, PartialReception: true, Layers: [3]LayerParameters{{4, 1, 4, 1}, {64, 2, 2, 12}}}
	d.p = &p
	d.started = true
	d.layers[0] = &layerDecoder{packets: 5}
	d.layers[1] = &layerDecoder{packets: 5}
	if !d.Locked() {
		t.Fatal("fixture mustlock")
	}
	// Synchronizer Reset is the exact internal coherence/pilot failure path.
	d.sync.Reset()
	d.PushIQ(nil, false)
	if d.Locked() || d.p != nil || d.started || d.layers[0] != nil {
		t.Fatal("internal synchronization loss retained transport history")
	}
}
func TestDecoderSameParametersReacquiresFreshLayers(t *testing.T) {
	d, _ := New(Config{})
	p := Parameters{Mode: 3, GuardDenominator: 8, PartialReception: true, Layers: [3]LayerParameters{{4, 1, 4, 1}, {64, 2, 2, 12}}}
	if _, e := d.consumeSymbol(nil, Symbol{Parameters: &p, FrameSymbol: 203}); e != nil {
		t.Fatal(e)
	}
	old := d.layers[0]
	old.packets = 20
	d.layers[1].packets = 20
	d.started = true
	d.sync.Reset()
	// This symbol represents fresh verification after a reset and reacquisition
	// within one PushIQ chunk, before its final generation check can run.
	if _, e := d.consumeSymbol(nil, Symbol{Parameters: &p, FrameSymbol: 203}); e != nil {
		t.Fatal(e)
	}
	if d.layers[0] == old || d.layers[0].packets != 0 || d.started || d.Locked() {
		t.Fatal("same parameters reused pre-loss FEC/TS history")
	}
	if _, e := d.consumeSymbol(nil, Symbol{Data: make([]complex64, 13*384), FrameSymbol: 0}); e != nil {
		t.Fatal(e)
	}
	if !d.started {
		t.Fatal("fresh frame did not restart layer processing")
	}
}

func TestDecoderPropagatesVerifiedUnsupportedTransmission(t *testing.T) {
	d, _ := New(Config{})
	p := Parameters{Mode: 3, GuardDenominator: 8}
	d.p = &p
	d.layers[0] = &layerDecoder{packets: 5}
	d.sync.err = ErrUnsupported
	_, err := d.PushIQ(nil, false)
	if err != ErrUnsupported {
		t.Fatalf("unsupported TMCC hidden: %v", err)
	}
	if d.Locked() {
		t.Fatal("unsupported verified transmission retained lock")
	}
}
