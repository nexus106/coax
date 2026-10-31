// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
// Package pcscnative accesses the operating system's PC/SC service without cgo.
package pcscnative

import (
	"context"
	"github.com/Till0196/coax/pkg/winscard"
)

func status(value uintptr) error {
	if uint32(value) == 0 {
		return nil
	}
	return winscard.Code(uint32(value))
}

// Connection is an exclusive conversation with a card. Implementations must not
// print APDUs: responses can contain keys and personal card information.
type Connection interface {
	Transmit(context.Context, []byte) ([]byte, error)
	Close() error
}

// Open uses the exact reader name, or the only available reader if name is empty.
func Open(ctx context.Context, name string) (Connection, error) { return open(ctx, name) }
