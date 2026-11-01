// Copyright 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
package msi2500

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/Till0196/coax/pkg/usbio"
)

// resetTransport uses the firmware reset, then reacquires the same physical USB
// port. MSi2500 retains FIFO/pipe state across close, so a new open needs this
// reset even though stopping and restarting an existing Bridge does not.
func resetTransport(t *usbio.Transport) (*usbio.Transport, error) {
	id := t.ID()
	list, err := usbio.Devices()
	if err != nil {
		return nil, err
	}
	var port, serial string
	for _, d := range list {
		if d.Location == t.Name() {
			port, serial = d.Port, d.Serial
			break
		}
	}
	// Refuse an ambiguous identity before touching hardware.
	if _, err = resetLocation(list, id, port, serial); err != nil {
		return nil, err
	}
	_, err = t.Control(0x42, 0x40, 0, 0, nil, 1000)
	closeErr := t.Close()
	if err != nil {
		return nil, fmt.Errorf("msi2500 firmware reset: %w", err)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		list, e := usbio.Devices()
		if e != nil {
			last = e
			continue
		}
		location, e := resetLocation(list, id, port, serial)
		if e != nil {
			last = e
			continue
		}
		// Firmware reset acknowledges before its USB disconnect is published.
		// Reopening the old Darwin registry entry can keep that stale device
		// alive with no interface, so wait for its replacement at the same port.
		if runtime.GOOS == "darwin" && location == t.Name() {
			last = errors.New("waiting for firmware-reset USB re-enumeration")
			continue
		}
		reopened, e := usbio.Open(location, []usbio.ID{id})
		if e == nil {
			return reopened, nil
		}
		last = e
	}
	return nil, fmt.Errorf("msi2500 reopen after firmware reset: %w", last)
}
func resetLocation(list []usbio.Attached, id usbio.ID, port, serial string) (string, error) {
	var matches []string
	for _, d := range list {
		if d.ID != id {
			continue
		}
		if port != "" && d.Port != port {
			continue
		}
		if port == "" && serial != "" && d.Serial != serial {
			continue
		}
		matches = append(matches, d.Location)
	}
	if len(matches) != 1 {
		return "", errors.New("msi2500: reset device not uniquely present at its original USB port")
	}
	return matches[0], nil
}
