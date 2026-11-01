// SPDX-License-Identifier: GPL-2.0-or-later
package msi2500

import (
	"encoding/binary"
	"testing"
)

func packet(counter uint32) []byte {
	b := make([]byte, 1024)
	binary.LittleEndian.PutUint32(b, counter)
	for j := 16; j < 1024; j += 3 {
		b[j] = 0xff
		b[j+1] = 0x07
		b[j+2] = 0x80
	}
	return b
}
func TestFrameConversion(t *testing.T) {
	var d decoder
	if err := d.push(packet(0)); err != nil {
		t.Fatal(err)
	}
	var dst [336]complex64
	n, loss := d.read(dst[:])
	if n != 336 || loss {
		t.Fatalf("%d %v", n, loss)
	}
	if dst[0] != complex(float32(2047)/2048, -1) {
		t.Fatalf("%v", dst[0])
	}
}
func TestSplitHeaderAndFrame(t *testing.T) {
	for _, cut := range []int{1, 3, 15, 16, 1023} {
		var d decoder
		b := packet(99)
		d.push(b[:cut])
		var dst [336]complex64
		if n, _ := d.read(dst[:]); n != 0 {
			t.Fatal(n)
		}
		d.push(b[cut:])
		if n, loss := d.read(dst[:]); n != 336 || loss {
			t.Fatalf("cut %d: %d %v", cut, n, loss)
		}
	}
}
func TestCounterWrapAndLoss(t *testing.T) {
	var d decoder
	var dst [336]complex64
	for i, c := range []uint32{0xffffff00, 80, 752} {
		d.push(packet(c))
		n, loss := d.read(dst[:])
		if n != 336 || loss != (i == 2) {
			t.Fatalf("%d: %d %v", i, n, loss)
		}
	}
}
func TestInvalidInputLength(t *testing.T) {
	var d decoder
	if err := d.push(make([]byte, 65537)); err == nil {
		t.Fatal("unbounded input accepted")
	}
}
