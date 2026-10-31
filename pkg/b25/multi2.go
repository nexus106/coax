// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0
// MULTI2 as specified by ARIB STD-B25 Part 1, Annex A. The arithmetic was
// checked against Apache-2.0 libarib25 (stz2012; MOGI, Kazuhiro).
package b25

import (
	"encoding/binary"
	"math/bits"
)

type block struct{ l, r uint32 }

func pi1(b block) block { b.r ^= b.l; return b }
func pi2(b block, k uint32) block {
	y := b.r + k
	z := bits.RotateLeft32(y, 1) + y - 1
	b.l ^= bits.RotateLeft32(z, 4) ^ z
	return b
}
func pi3(b block, k, j uint32) block {
	y := b.l + k
	z := bits.RotateLeft32(y, 2) + y + 1
	a := (bits.RotateLeft32(z, 8) ^ z) + j
	c := a + (a >> 31)
	b.r ^= bits.RotateLeft32(c, 16) ^ (c | b.l)
	return b
}
func pi4(b block, k uint32) block { y := b.r + k; b.l ^= bits.RotateLeft32(y, 2) + y + 1; return b }
func load(p []byte) block         { return block{binary.BigEndian.Uint32(p), binary.BigEndian.Uint32(p[4:])} }
func (b block) store(p []byte) {
	binary.BigEndian.PutUint32(p, b.l)
	binary.BigEndian.PutUint32(p[4:], b.r)
}
func (b block) xor(c block) block { return block{b.l ^ c.l, b.r ^ c.r} }

type multi2 struct {
	work [8]uint32
	iv   block
}

func newMulti2(system [32]byte, key, iv [8]byte) *multi2 {
	c := &multi2{iv: load(iv[:])}
	var s [8]uint32
	for i := range s {
		s[i] = binary.BigEndian.Uint32(system[i*4:])
	}
	b := pi1(load(key[:]))
	b = pi2(b, s[0])
	c.work[0] = b.l
	b = pi3(b, s[1], s[2])
	c.work[1] = b.r
	b = pi4(b, s[3])
	c.work[2] = b.l
	b = pi1(b)
	c.work[3] = b.r
	b = pi2(b, s[4])
	c.work[4] = b.l
	b = pi3(b, s[5], s[6])
	c.work[5] = b.r
	b = pi4(b, s[7])
	c.work[6] = b.l
	b = pi1(b)
	c.work[7] = b.r
	return c
}
func (c *multi2) crypt(b block, decrypt bool) block {
	k := c.work
	for i := 0; i < 4; i++ {
		if decrypt {
			b = pi4(b, k[7])
			b = pi3(b, k[5], k[6])
			b = pi2(b, k[4])
			b = pi1(b)
			b = pi4(b, k[3])
			b = pi3(b, k[1], k[2])
			b = pi2(b, k[0])
			b = pi1(b)
		} else {
			b = pi1(b)
			b = pi2(b, k[0])
			b = pi3(b, k[1], k[2])
			b = pi4(b, k[3])
			b = pi1(b)
			b = pi2(b, k[4])
			b = pi3(b, k[5], k[6])
			b = pi4(b, k[7])
		}
	}
	return b
}
func (c *multi2) payload(p []byte, decrypt bool) {
	state := c.iv
	for len(p) >= 8 {
		b := load(p)
		if decrypt {
			c.crypt(b, true).xor(state).store(p)
			state = b
		} else {
			state = c.crypt(b.xor(state), false)
			state.store(p)
		}
		p = p[8:]
	}
	if len(p) > 0 {
		var tail [8]byte
		c.crypt(state, false).store(tail[:])
		for i := range p {
			p[i] ^= tail[i]
		}
	}
}
func (c *multi2) decrypt(p []byte) { c.payload(p, true) }
func (c *multi2) encrypt(p []byte) { c.payload(p, false) }
