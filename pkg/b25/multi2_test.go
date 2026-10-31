// SPDX-License-Identifier: Apache-2.0
package b25

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestMULTI2CBCAndTails(t *testing.T) {
	var system [32]byte
	var key, iv [8]byte
	for i := range system {
		system[i] = byte(i)
	}
	for i := range key {
		key[i] = byte(i + 32)
		iv[i] = byte(i + 40)
	}
	// Independent libarib25 oracle, with synthetic material only.
	want := []byte{0x54, 0xa7, 0x1c, 0x20, 0xd0, 0xcd, 0x15, 0x9e}
	c := newMulti2(system, key, iv)
	p := make([]byte, 8)
	c.encrypt(p)
	if !bytes.Equal(p, want) {
		t.Fatalf("known block mismatch: %x", p)
	}
	gold, _ := hex.DecodeString("17b09df1375a1a016d9642df8ed81053effb23a45b1477")
	c.decrypt(gold)
	for i, b := range gold {
		if b != byte(i) {
			t.Fatal("independent CBC/OFB tail vector mismatch")
		}
	}
	for n := 1; n <= 184; n++ {
		p := make([]byte, n)
		for i := range p {
			p[i] = byte(i)
		}
		orig := bytes.Clone(p)
		c.encrypt(p)
		c.decrypt(p)
		if !bytes.Equal(p, orig) {
			t.Fatalf("CBC/OFB length %d", n)
		}
	}
}
