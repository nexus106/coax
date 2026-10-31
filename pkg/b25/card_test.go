// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package b25

import (
	"context"
	"errors"
	"github.com/Till0196/coax/pkg/winscard"
	"testing"
)

type fakeTransport struct {
	response []byte
	err      error
}

func (f *fakeTransport) Transmit(context.Context, []byte) ([]byte, error) { return f.response, f.err }
func (f *fakeTransport) Close() error                                     { return nil }
func TestCardRejectsRefusalAndRemoval(t *testing.T) {
	f := &fakeTransport{response: make([]byte, 25)}
	f.response[23] = 0x90
	c := &casCard{connection: f}
	if _, err := c.ProcessECM(context.Background(), []byte{1}); !errors.Is(err, ErrRefused) {
		t.Fatalf("refusal: %v", err)
	}
	f.err = winscard.ErrRemovedCard
	if _, err := c.ProcessECM(context.Background(), []byte{1}); !errors.Is(err, winscard.ErrRemovedCard) {
		t.Fatalf("removed: %v", err)
	}
}
