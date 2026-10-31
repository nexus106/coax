// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeOptionsValidatedBeforeOpeningCard(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{{[]string{"decode"}, "--input"}, {[]string{"decode", "--input", "x", "extra"}, "argument"}, {[]string{"decode", "--input", "same.ts", "-o", "same.ts"}, "different"}, {[]string{"tune", "--card-reader", "reader"}, "--b25"}} {
		var out, errOut bytes.Buffer
		err := Run(tc.args, strings.NewReader(""), &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.args, err)
		}
	}
}
