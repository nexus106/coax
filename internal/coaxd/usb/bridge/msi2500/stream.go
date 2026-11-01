// Copyright 2013 Miroslav Slugen; 2025 Peter Hackenberg; 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
// Register programming adapted from libmirisdr-5 reg.c, adc.c, hard.c,
// soft.c and gain.c, distributed under GPL-2.0-or-later. No GPL-3-only source.

package msi2500

import (
	"context"
	"errors"
	"github.com/Till0196/coax/pkg/usbio"
	"time"
)

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
