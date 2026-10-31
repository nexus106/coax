// Copyright 2013 Miroslav Slugen; 2025 Peter Hackenberg; 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
// Register programming adapted from libmirisdr-5 reg.c, adc.c, hard.c,
// soft.c and gain.c, distributed under GPL-2.0-or-later. No GPL-3-only source.

// Package msi2500 controls the MSi2500 USB ADC and MSi001 UHF tuner.
// The stream is fixed to 336 packed signed 12-bit complex samples per frame,
// 8,126,984 samples/s, zero IF, 8 MHz filter, alternate 3 and bulk IN 0x81.
package msi2500

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Till0196/coax/pkg/usbio"
)

const SampleRate = 8126984

var ErrStopped = errors.New("msi2500: stream stopped")

type device interface {
	Control(byte, byte, uint16, uint16, []byte, uint32) (int, error)
	SetAlternate(int, int) error
	Close() error
}
type Bridge struct {
	usb        device
	stream     usbio.EndpointReader
	opMu       sync.Mutex
	readMu     sync.Mutex
	running    atomic.Bool
	closed     bool
	tuned      bool
	decoder    decoder
	pendingErr error
	buffer     [65536]byte
}

// New takes ownership of an already opened transport, resets firmware and
// reopens the same physical USB port. Nothing streams until Tune and Start succeed.
func New(t *usbio.Transport) (*Bridge, error) {
	if t == nil {
		return nil, errors.New("msi2500: nil transport")
	}
	reopened, err := resetTransport(t)
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	b, err := newBridge(reopened, reopened.BulkReader(0x81))
	if err != nil {
		_ = reopened.Close()
	}
	return b, err
}
func newBridge(t device, r usbio.EndpointReader) (*Bridge, error) {
	b := &Bridge{usb: t, stream: r}
	if err := b.command(0x45); err != nil {
		return nil, err
	}
	if err := t.SetAlternate(0, 3); err != nil {
		return nil, fmt.Errorf("msi2500 alternate 3: %w", err)
	}
	if err := b.reg(3, 0x010000); err != nil {
		return nil, err
	}
	for _, v := range [][2]uint32{{8, 0x6080}, {5, 12}, {0, 0x200}, {2, 0x4801}, {8, 0xf380}} {
		if err := b.reg(byte(v[0]), v[1]); err != nil {
			return nil, err
		}
	}
	if err := b.rate(); err != nil {
		return nil, err
	}
	return b, nil
}
func (b *Bridge) command(r byte) error {
	n, err := b.usb.Control(0x42, r, 0, 0, nil, 1000)
	if err != nil {
		return fmt.Errorf("msi2500 command %#x: %w", r, err)
	}
	if n != 0 {
		return errors.New("msi2500: unexpected control response length")
	}
	return nil
}
func (b *Bridge) reg(r byte, v uint32) error {
	n, err := b.usb.Control(0x42, 0x41, uint16(v&255)<<8|uint16(r), uint16(v>>8), nil, 1000)
	if err != nil {
		return fmt.Errorf("msi2500 register %#x: %w", r, err)
	}
	if n != 0 {
		return errors.New("msi2500: unexpected register response length")
	}
	return nil
}
func (b *Bridge) rate() error {
	// 48 MHz reference, ADC VCO >=202 MHz, 21-bit fractional PLL.
	const divider = 4
	const vco = uint64(SampleRate) * divider * 12
	const fraction = (vco % 48000000) * 0x200000 / 48000000
	const reg3 = 3 | (divider/2-1)<<2 | ((fraction>>20)&1)<<7 | ((vco/48000000)&15)<<8 | 5<<12 | 1<<16
	for _, v := range [][2]uint32{{7, 0x85}, {4, uint32(fraction & 0xfffff)}, {3, uint32(reg3)}} {
		if err := b.reg(byte(v[0]), v[1]); err != nil {
			return err
		}
	}
	return nil
}

// Tune accepts UHF frequencies [330,960) MHz and actual manual gain 0..85 dB.
// MSi001's UHF LNA contributes 7 dB, mixer 19 dB, baseband 0..59 dB.
// Retuning a running stream requires Stop first.
func (b *Bridge) Tune(f uint32, gain int) error {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.closed {
		return usbio.ErrGone
	}
	if b.running.Load() {
		return errors.New("msi2500: stop before tuning")
	}
	if f < 330000000 || f >= 960000000 {
		return errors.New("msi2500: frequency outside supported UHF band")
	}
	if gain < 0 || gain > 85 {
		return errors.New("msi2500: UHF gain must be 0..85 dB")
	}
	const div = uint64(4)
	fvco := uint64(f) * div
	n := fvco / 96000000
	thresh := uint64(96000000) / div
	frac := (fvco % 96000000) / div
	a, c := thresh, frac
	for c != 0 {
		a, c = c, a%c
	}
	thresh /= a
	frac /= a
	scale := (thresh + 4094) / 4095
	thresh = (thresh + scale/2) / scale
	frac = (frac + scale/2) / scale
	actual := 96000000 * (n*thresh + frac) / (thresh * div)
	if uint64(f) < actual {
		frac--
	}
	actual = 96000000 * (n*thresh + frac) / (thresh * div)
	afc := (uint64(f) - actual) * thresh * 4096 * div / 96000000
	lna, mixer, bb := uint32(1), uint32(1), gain
	if gain >= 26 {
		lna = 0
		mixer = 0
		bb -= 26
	} else if gain >= 19 {
		mixer = 0
		bb -= 19
	}
	regs := [][2]uint32{{8, 0xf380}, {9, 0x0e}, {9, 3 | uint32(afc&4095)<<4}, {9, 8<<4 | 1<<10 | 3<<12 | 7<<14 | 2<<17}, {9, 5 | uint32(thresh)<<4 | 0x28<<16}, {9, 2 | uint32(frac&4095)<<4 | uint32(n&63)<<16}, {9, 0x0d}, {9, 1 | uint32(59-bb)<<4 | mixer<<12 | lna<<13 | 2<<14}, {9, 6 | 0x1f<<4 | 0x800<<10}}
	for _, v := range regs {
		if err := b.reg(byte(v[0]), v[1]); err != nil {
			b.tuned = false
			return err
		}
	}
	b.tuned = true
	return nil
}
func (b *Bridge) Start() error {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.closed {
		return usbio.ErrGone
	}
	if b.running.Load() {
		return nil
	}
	if !b.tuned {
		return errors.New("msi2500: tune before starting")
	}
	b.readMu.Lock()
	defer b.readMu.Unlock()
	b.decoder = decoder{}
	b.pendingErr = nil
	if err := b.stream.Start(); err != nil {
		return err
	}
	if err := b.command(0x43); err != nil {
		_ = b.stream.Stop()
		return err
	}
	b.running.Store(true)
	return nil
}

// ReadIQ returns normalized IQ. A true discontinuity starts a new contiguous
// interval at dst[0]. It never combines samples across a missing frame.
// A large destination receives all complete contiguous frames buffered by the
// current USB transfer (up to21,504 samples for a64KiB transfer).
// One caller may read; Stop/Close may run concurrently. Cancellation is checked
// between bounded 50 ms USB polls and returns ctx.Err().
func (b *Bridge) ReadIQ(ctx context.Context, dst []complex64) (int, bool, error) {
	b.readMu.Lock()
	defer b.readMu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		if !b.running.Load() {
			return 0, false, ErrStopped
		}
		if len(dst) == 0 {
			return 0, false, nil
		}
		if n, loss := b.decoder.read(dst); n > 0 {
			return n, loss, nil
		}
		if b.pendingErr != nil {
			err := b.pendingErr
			b.pendingErr = nil
			return 0, false, err
		}
		n, err := b.stream.Read(b.buffer[:], 50)
		if n < 0 || n > len(b.buffer) {
			return 0, false, errors.New("msi2500: invalid USB read length")
		}
		if n > 0 {
			if err != nil && !errors.Is(err, usbio.ErrTimeout) {
				b.pendingErr = err
			}
			if e := b.decoder.push(b.buffer[:n]); e != nil {
				return 0, false, e
			}
			continue
		}
		if err != nil && !errors.Is(err, usbio.ErrTimeout) {
			return 0, false, err
		}
	}
}
func (b *Bridge) stop() error {
	b.running.Store(false)
	b.readMu.Lock()
	defer b.readMu.Unlock()
	b.decoder = decoder{}
	b.pendingErr = nil
	err := b.stream.Stop()
	// Let the USB FIFO settle after withdrawing in-flight transfers.
	time.Sleep(20 * time.Millisecond)
	return errors.Join(err, b.command(0x45))
}
func (b *Bridge) Stop() error {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.closed {
		return nil
	}
	return b.stop()
}
func (b *Bridge) Close() error {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return errors.Join(b.stop(), b.usb.Close())
}
