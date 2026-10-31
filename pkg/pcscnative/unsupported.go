//go:build !darwin

// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package pcscnative

import (
	"context"
	"fmt"
)

func open(context.Context, string) (Connection, error) {
	return nil, fmt.Errorf("native PC/SC is unsupported on this operating system")
}
