// Copyright 2013 Miroslav Slugen; 2025 Peter Hackenberg; 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
// Register programming adapted from libmirisdr-5 reg.c, adc.c, hard.c,
// soft.c and gain.c, distributed under GPL-2.0-or-later. No GPL-3-only source.

// Package msi2500 controls the MSi2500 USB ADC and its tuner register bus.
// The stream is fixed to 336 packed signed 12-bit complex samples per frame,
// 8,126,984 samples/s, zero IF, 8 MHz filter, alternate 3 and bulk IN 0x81.
package msi2500

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

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
	if err := b.configureADCClock(); err != nil {
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
