// SPDX-License-Identifier: GPL-2.0-or-later
package msi2500

import (
	"github.com/Till0196/coax/pkg/usbio"
	"testing"
)

func TestSelectResetDevice(t *testing.T) {
	id := usbio.ID{Vendor: 0x1df7, Product: 0x2500}
	list := []usbio.Attached{{ID: id, Location: "new1", Port: "A"}, {ID: id, Location: "new2", Port: "B"}}
	if v, e := resetLocation(list, id, "B", ""); e != nil || v != "new2" {
		t.Fatalf("%s %v", v, e)
	}
	if _, e := resetLocation(list, id, "C", ""); e == nil {
		t.Fatal("another socket chosen")
	}
	if _, e := resetLocation(list, id, "", ""); e == nil {
		t.Fatal("ambiguous device chosen")
	}
}
