// SPDX-License-Identifier: GPL-2.0-or-later
package msi2500

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Till0196/coax/internal/coaxd/tuner/msi001"
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
	if err = msi001.New(b.TunerBus()).Tune(539142857, 15); err != nil {
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
		if msi001.New(b.TunerBus()).Tune(v.f, v.g) == nil {
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
	if e = msi001.New(b.TunerBus()).Tune(539142857, 15); e != nil {
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
	msi001.New(b.TunerBus()).Tune(539142857, 15)
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

type chunkReader struct {
	chunks [][]byte
	reads  int
}

func (r *chunkReader) Start() error { return nil }
func (r *chunkReader) Stop() error  { return nil }
func (r *chunkReader) Read(dst []byte, timeout uint32) (int, error) {
	r.reads++
	if len(r.chunks) == 0 {
		return 0, usbio.ErrGone
	}
	b := r.chunks[0]
	r.chunks = r.chunks[1:]
	return copy(dst, b), nil
}
func batchBridge(t *testing.T, r usbio.EndpointReader) *Bridge {
	t.Helper()
	b, e := newBridge(new(fakeDevice), r)
	if e != nil {
		t.Fatal(e)
	}
	if e = msi001.New(b.TunerBus()).Tune(539142857, 15); e != nil {
		t.Fatal(e)
	}
	if e = b.Start(); e != nil {
		t.Fatal(e)
	}
	return b
}
func frames(counters ...uint32) []byte {
	var b []byte
	for _, c := range counters {
		b = append(b, packet(c)...)
	}
	return b
}
func TestReadIQBatchesOneUSBTransfer(t *testing.T) {
	r := &chunkReader{chunks: [][]byte{frames(0, 336, 672), frames(1008)}}
	b := batchBridge(t, r)
	dst := make([]complex64, 32768)
	n, loss, e := b.ReadIQ(context.Background(), dst)
	if e != nil || loss || n != 1008 || r.reads != 1 {
		t.Fatalf("n=%d loss=%v err=%v USBreads=%d", n, loss, e, r.reads)
	}
	for _, v := range dst[:n] {
		if v != complex(float32(2047)/2048, -1) {
			t.Fatal(v)
		}
	}
}
func TestReadIQBatchStopsBeforeGap(t *testing.T) {
	r := &chunkReader{chunks: [][]byte{frames(0, 336, 1008, 1344)}}
	b := batchBridge(t, r)
	dst := make([]complex64, 32768)
	for i, wantLoss := range []bool{false, true} {
		n, loss, e := b.ReadIQ(context.Background(), dst)
		if e != nil || loss != wantLoss || n != 672 {
			t.Fatalf("part%d n=%d loss=%v err=%v", i, n, loss, e)
		}
	}
	if r.reads != 1 {
		t.Fatal(r.reads)
	}
}
func TestReadIQBatchCounterWrapAndFragmentedHeader(t *testing.T) {
	raw := frames(0xffffff00, 80, 416)
	r := &chunkReader{chunks: [][]byte{raw[:13], raw[13:2061], raw[2061:]}}
	b := batchBridge(t, r)
	dst := make([]complex64, 32768)
	n, loss, e := b.ReadIQ(context.Background(), dst)
	if e != nil || loss || n != 672 || r.reads != 2 {
		t.Fatalf("n=%d loss=%v err=%v reads=%d", n, loss, e, r.reads)
	}
	n, loss, e = b.ReadIQ(context.Background(), dst)
	if e != nil || loss || n != 336 || r.reads != 3 {
		t.Fatalf("tail n=%d loss=%v err=%v reads=%d", n, loss, e, r.reads)
	}
}
func TestReadIQBatchSmallDestinationAndCancel(t *testing.T) {
	r := &chunkReader{chunks: [][]byte{frames(0, 336, 1008)}}
	b := batchBridge(t, r)
	dst := make([]complex64, 500)
	n, loss, e := b.ReadIQ(context.Background(), dst)
	if n != 500 || loss || e != nil {
		t.Fatalf("%d %v %v", n, loss, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, _, e = b.ReadIQ(ctx, dst); n != 0 || !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled n=%d err=%v", n, e)
	}
	n, loss, e = b.ReadIQ(context.Background(), dst)
	if n != 172 || loss || e != nil {
		t.Fatalf("beforegap %d %v %v", n, loss, e)
	}
	n, loss, e = b.ReadIQ(context.Background(), dst)
	if n != 336 || !loss || e != nil {
		t.Fatalf("gap %d %v %v", n, loss, e)
	}
}
