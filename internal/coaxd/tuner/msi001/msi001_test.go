// Copyright 2026 nexus106
// SPDX-License-Identifier: GPL-2.0-or-later

package msi001

import (
	"errors"
	"reflect"
	"testing"
)

type fakeBus struct {
	words []uint32
	calls int
	err   error
}

func (b *fakeBus) WriteRegisters(words []uint32) error {
	b.calls++
	b.words = append([]uint32(nil), words...)
	return b.err
}

// Register snapshots from the original, hardware-verified combined driver.
// Keep its PLL rounding and gain switches unchanged when separating the chips.
func TestTunePreservesRegisterSequences(t *testing.T) {
	cases := []struct {
		frequency uint32
		gain      int
		want      []uint32
	}{
		{473142857, 15, []uint32{0xe, 0x3, 0x5f480, 0x28fff5, 0x13b6d2, 0xd, 0xb2c1, 0x2001f6}},
		{539142857, 15, []uint32{0xe, 0x4003, 0x5f480, 0x28fff5, 0x1676d2, 0xd, 0xb2c1, 0x2001f6}},
		{491142857, 5, []uint32{0xe, 0x4003, 0x5f480, 0x28fff5, 0x1476d2, 0xd, 0xb361, 0x2001f6}},
		{503142857, 25, []uint32{0xe, 0xc003, 0x5f480, 0x28fff5, 0x14f6c2, 0xd, 0xa351, 0x2001f6}},
		{539142857, 19, []uint32{0xe, 0x4003, 0x5f480, 0x28fff5, 0x1676d2, 0xd, 0xa3b1, 0x2001f6}},
		{539142857, 26, []uint32{0xe, 0x4003, 0x5f480, 0x28fff5, 0x1676d2, 0xd, 0x83b1, 0x2001f6}},
		{330000000, 0, []uint32{0xe, 0x3, 0x5f480, 0x280045, 0xd0032, 0xd, 0xb3b1, 0x2001f6}},
		{959999999, 85, []uint32{0xe, 0xfff3, 0x5f480, 0x28fff5, 0x27ffe2, 0xd, 0x8001, 0x2001f6}},
	}
	for _, c := range cases {
		b := new(fakeBus)
		tuner := New(b)
		if e := tuner.Tune(c.frequency, c.gain); e != nil {
			t.Fatal(e)
		}
		if b.calls != 1 || !reflect.DeepEqual(b.words, c.want) {
			t.Fatalf("%dHz/%ddB: calls=%d words=%#v want=%#v", c.frequency, c.gain, b.calls, b.words, c.want)
		}
	}
}
func TestInvalidTuneDoesNotTouchBus(t *testing.T) {
	for _, c := range []struct {
		f uint32
		g int
	}{{0, 15}, {329999999, 15}, {960000000, 15}, {539142857, -1}, {539142857, 86}} {
		b := new(fakeBus)
		if e := New(b).Tune(c.f, c.g); e == nil {
			t.Fatalf("accepted %+v", c)
		}
		if b.calls != 0 {
			t.Fatal("invalid tune wrote hardware")
		}
	}
}
func TestTunePreservesBusError(t *testing.T) {
	sentinel := errors.New("SPI transfer failed")
	b := &fakeBus{err: sentinel}
	if e := New(b).Tune(539142857, 15); !errors.Is(e, sentinel) {
		t.Fatalf("got %v", e)
	}
}
