// Copyright 2013 Miroslav Slugen; 2025 Peter Hackenberg; 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
// Register programming adapted from libmirisdr-5 reg.c, adc.c, hard.c,
// soft.c and gain.c, distributed under GPL-2.0-or-later. No GPL-3-only source.

package msi2500

import (
	"errors"
	"github.com/Till0196/coax/pkg/usbio"
)

// TunerBus carries serial register writes to the RF tuner behind the bridge.
// A complete configuration is serialized with stream start/stop operations.
type TunerBus struct{ bridge *Bridge }

func (b *Bridge) TunerBus() *TunerBus { return &TunerBus{bridge: b} }

func (bus *TunerBus) WriteRegisters(values []uint32) error {
	b := bus.bridge
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.closed {
		return usbio.ErrGone
	}
	if b.running.Load() {
		return errors.New("msi2500: stop before tuning")
	}
	b.tuned = false
	// Prepare the ADC/FIFO before issuing serial writes to the tuner.
	if err := b.reg(8, 0xf380); err != nil {
		return err
	}
	for _, value := range values {
		if err := b.reg(9, value); err != nil {
			return err
		}
	}
	b.tuned = true
	return nil
}
