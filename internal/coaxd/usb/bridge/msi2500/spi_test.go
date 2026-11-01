// Copyright 2026 nexus106
// SPDX-License-Identifier: GPL-2.0-or-later

package msi2500

import (
	"errors"
	"reflect"
	"testing"
)

func TestTunerBusPreservesUSBRegisterOrder(t *testing.T) {
	d := new(fakeDevice)
	b, e := newBridge(d, new(fakeReader))
	if e != nil {
		t.Fatal(e)
	}
	d.regs = nil
	if e = b.TunerBus().WriteRegisters([]uint32{0xe, 0xb2c1}); e != nil {
		t.Fatal(e)
	}
	want := [][2]uint32{{8, 0xf380}, {9, 0xe}, {9, 0xb2c1}}
	if !reflect.DeepEqual(d.regs, want) {
		t.Fatalf("got%x want%x", d.regs, want)
	}
	if e = b.Start(); e != nil {
		t.Fatal(e)
	}
	before := len(d.regs)
	if e = b.TunerBus().WriteRegisters([]uint32{0xe}); e == nil {
		t.Fatal("programmed a running tuner")
	}
	if len(d.regs) != before {
		t.Fatal("busy bus wrote registers")
	}
	b.Close()
	if e = b.TunerBus().WriteRegisters([]uint32{0xe}); e == nil {
		t.Fatal("programmed a closed bridge")
	}
}

type failingDevice struct {
	fakeDevice
	fail   bool
	writes int
}

func (d *failingDevice) Control(rt, r byte, v, i uint16, data []byte, timeout uint32) (int, error) {
	if d.fail && r == 0x41 {
		d.writes++
		if d.writes == 3 {
			return 0, errSPIWrite
		}
	}
	return d.fakeDevice.Control(rt, r, v, i, data, timeout)
}

var errSPIWrite = errors.New("SPI register failed")

func TestFailedTunerTransferRevokesStreamReadiness(t *testing.T) {
	d := new(failingDevice)
	b, e := newBridge(d, new(fakeReader))
	if e != nil {
		t.Fatal(e)
	}
	if e = b.TunerBus().WriteRegisters([]uint32{0xe}); e != nil {
		t.Fatal(e)
	}
	d.fail = true
	if e = b.TunerBus().WriteRegisters([]uint32{0xe, 0x4003, 0x5f480}); !errors.Is(e, errSPIWrite) {
		t.Fatal(e)
	}
	if e = b.Start(); e == nil {
		t.Fatal("started after failed tuner programming")
	}
	if d.writes != 3 {
		t.Fatal("transfer continued after failure")
	}
}
