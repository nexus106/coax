// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package pcscnative

import (
	"errors"
	"github.com/Till0196/coax/pkg/winscard"
	"testing"
)

func TestNativeStatusPreservesCardErrors(t *testing.T) {
	for _, code := range []winscard.Code{winscard.ErrNoSmartcard, winscard.ErrRemovedCard} {
		if !errors.Is(status(uintptr(code)), code) {
			t.Fatal("lost PCSC status")
		}
	}
	if status(0) != nil {
		t.Fatal("success")
	}
}
