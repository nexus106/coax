// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
// Package b25 descrambles MPEG transport streams using a legitimate B-CAS card.
// APDU layouts and MULTI2 parameters follow ARIB STD-B25. No EMM is sent.
package b25

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/Till0196/coax/pkg/pcscnative"
)

var ErrRefused = errors.New("B-CAS card refused entitlement")
var ErrMissingECM = errors.New("missing usable ECM for scrambled packet")

// Keys holds transient odd and even control words. Never log or persist it.
type Keys struct{ Odd, Even [8]byte }

// Card supplies initial MULTI2 parameters and processes ECMs. Callers own Close.
type Card interface {
	Parameters() ([32]byte, [8]byte)
	ProcessECM(context.Context, []byte) (Keys, error)
	Close() error
}
type casCard struct {
	connection pcscnative.Connection
	system     [32]byte
	iv         [8]byte
}

func OpenCard(ctx context.Context, reader string) (Card, error) {
	c, err := pcscnative.Open(ctx, reader)
	if err != nil {
		return nil, err
	}
	card := &casCard{connection: c}
	r, err := c.Transmit(ctx, []byte{0x90, 0x30, 0, 0, 0})
	if err != nil {
		c.Close()
		return nil, err
	}
	if len(r) < 58 || !success(r) || binary.BigEndian.Uint16(r[4:]) != 0x2100 {
		c.Close()
		return nil, fmt.Errorf("B-CAS initialization rejected")
	}
	copy(card.system[:], r[16:48])
	copy(card.iv[:], r[48:56])
	clear(r)
	return card, nil
}
func success(r []byte) bool                        { return len(r) >= 2 && r[len(r)-2] == 0x90 && r[len(r)-1] == 0 }
func (c *casCard) Parameters() ([32]byte, [8]byte) { return c.system, c.iv }
func (c *casCard) ProcessECM(ctx context.Context, ecm []byte) (Keys, error) {
	var k Keys
	if len(ecm) == 0 || len(ecm) > 255 {
		return k, fmt.Errorf("B-CAS ECM length must be 1..255 bytes")
	}
	command := make([]byte, len(ecm)+6)
	copy(command, []byte{0x90, 0x34, 0, 0, byte(len(ecm))})
	copy(command[5:], ecm)
	r, err := c.connection.Transmit(ctx, command)
	if err != nil {
		return k, err
	}
	defer clear(r)
	if len(r) < 25 || !success(r) {
		return k, fmt.Errorf("invalid B-CAS ECM response")
	}
	code := binary.BigEndian.Uint16(r[4:])
	if code != 0x0800 && code != 0x0400 && code != 0x0200 {
		return k, ErrRefused
	}
	copy(k.Odd[:], r[6:14])
	copy(k.Even[:], r[14:22])
	return k, nil
}
func (c *casCard) Close() error { clear(c.system[:]); clear(c.iv[:]); return c.connection.Close() }
