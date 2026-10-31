// SPDX-License-Identifier: GPL-2.0-or-later
package msi2500

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Till0196/coax/pkg/usbio"
)

type fakeDevice struct {
	regs      [][2]uint32
	commands  []byte
	alternate int
}

func (f *fakeDevice) Control(rt, r byte, v, i uint16, b []byte, tm uint32) (int, error) {
	if rt != 0x42 {
		return 0, errors.New("request type")
	}
	f.commands = append(f.commands, r)
	if r == 0x41 {
		f.regs = append(f.regs, [2]uint32{uint32(v & 255), uint32(v>>8) | uint32(i)<<8})
	}
	return 0, nil
}
func (f *fakeDevice) SetAlternate(i, a int) error { f.alternate = a; return nil }
func (f *fakeDevice) Close() error                { return nil }

type fakeReader struct{ started bool }

func (f *fakeReader) Start() error                          { f.started = true; return nil }
func (f *fakeReader) Stop() error                           { f.started = false; return nil }
func (f *fakeReader) Read(b []byte, tm uint32) (int, error) { return 0, usbio.ErrTimeout }
func TestControlAndLifecycle(t *testing.T) {
	f := new(fakeDevice)
	r := new(fakeReader)
	b, err := newBridge(f, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Tune(539142857, 15); err != nil {
		t.Fatal(err)
	}
	// 15 dB = baseband reduction 44, mixer/LNA disabled; periodic DC mode 2.
	want := uint32(1 | 44<<4 | 1<<12 | 1<<13 | 2<<14)
	found := false
	for _, reg := range f.regs {
		if reg == [2]uint32{9, want} {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing gain register %#x: %x", want, f.regs)
	}
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	if f.alternate != 3 {
		t.Fatal(f.alternate)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = b.ReadIQ(ctx, make([]complex64, 336)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = b.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = b.ReadIQ(context.Background(), make([]complex64, 336)); !errors.Is(err, ErrStopped) {
		t.Fatal(err)
	}
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	if err = b.Start(); err == nil {
		t.Fatal("closed bridge started")
	}
}
func TestTuneRange(t *testing.T) {
	b, _ := newBridge(new(fakeDevice), new(fakeReader))
	for _, v := range []struct {
		f uint32
		g int
	}{{0, 15}, {539142857, -1}, {539142857, 86}} {
		if b.Tune(v.f, v.g) == nil {
			t.Fatal(v)
		}
	}
}

func TestADCClockConfiguredBeforeTuner(t *testing.T) {
	f := new(fakeDevice)
	b, e := newBridge(f, new(fakeReader))
	if e != nil {
		t.Fatal(e)
	}
	if e = b.Tune(539142857, 15); e != nil {
		t.Fatal(e)
	}
	clock := false
	for _, r := range f.regs {
		if r == [2]uint32{3, 0x15807} {
			clock = true
		}
		if r[0] == 9 && !clock {
			t.Fatal("tuner programmed before ADC clock")
		}
	}
}

type finalReader struct{ data []byte }

func (f *finalReader) Start() error { return nil }
func (f *finalReader) Stop() error  { return nil }
func (f *finalReader) Read(dst []byte, m uint32) (int, error) {
	if len(f.data) == 0 {
		return 0, usbio.ErrTimeout
	}
	n := copy(dst, f.data)
	f.data = nil
	return n, usbio.ErrGone
}
func TestReadPreservesErrorAlongsideData(t *testing.T) {
	b, e := newBridge(new(fakeDevice), &finalReader{data: packet(0)})
	if e != nil {
		t.Fatal(e)
	}
	b.Tune(539142857, 15)
	b.Start()
	var dst [336]complex64
	if n, _, e := b.ReadIQ(context.Background(), dst[:]); n != 336 || e != nil {
		t.Fatalf("%d %v", n, e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, e := b.ReadIQ(ctx, dst[:]); !errors.Is(e, usbio.ErrGone) {
		t.Fatal(e)
	}
}
