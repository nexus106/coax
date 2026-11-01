// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package dtv021tu

import (
	"context"
	"errors"
	"testing"
	"time"

	demod "github.com/Till0196/coax/internal/coaxd/demod/msi2500"
	"github.com/Till0196/coax/internal/coaxd/device"
	"github.com/Till0196/coax/pkg/usbio"
)

type fakeBridge struct {
	started, stopped bool
	fail             error
	closed           chan struct{}
}

func (b *fakeBridge) Tune(uint32, int) error { return b.fail }
func (b *fakeBridge) Start() error           { b.started = true; return b.fail }
func (b *fakeBridge) Stop() error {
	if !b.stopped {
		b.stopped = true
		close(b.closed)
	}
	return nil
}
func (b *fakeBridge) ReadIQ(ctx context.Context, dst []complex64) (int, bool, error) {
	select {
	case <-ctx.Done():
		return 0, false, ctx.Err()
	case <-b.closed:
		return 0, false, context.Canceled
	case <-time.After(time.Millisecond):
		dst[0] = 1
		return 1, false, nil
	}
}

type fakeProcessor struct{ n int }

func (p *fakeProcessor) PushIQ([]complex64, bool) ([]byte, error) {
	p.n++
	b := make([]byte, 188)
	b[0] = 0x47
	return b, nil
}
func (p *fakeProcessor) Locked() bool { return p.n > 0 }
func (p *fakeProcessor) Snapshot() demod.SyncSnapshot {
	return demod.SyncSnapshot{Parameters: &demod.Parameters{Mode: 3}}
}

func TestTuneRejectsUnsupportedBeforeStarting(t *testing.T) {
	b := &fakeBridge{closed: make(chan struct{})}
	f := newFrontend(b, b, func() (processor, error) { return &fakeProcessor{}, nil })
	if err := f.Tune(map[string]string{"DELIVERY_SYSTEM": "DVBS", "FREQUENCY": "539142857"}); err == nil {
		t.Fatal("satellite accepted")
	}
	if b.started {
		t.Fatal("unsupported tune started hardware")
	}
	if err := f.Tune(map[string]string{"DELIVERY_SYSTEM": "ISDBT", "FREQUENCY": "100"}); err == nil {
		t.Fatal("out of range accepted")
	}
}
func TestTuneRunsBeforeOpenAndReleaseStopsIt(t *testing.T) {
	b := &fakeBridge{closed: make(chan struct{})}
	f := newFrontend(b, b, func() (processor, error) { return &fakeProcessor{}, nil })
	if err := f.Tune(map[string]string{"DELIVERY_SYSTEM": "ISDBT", "FREQUENCY": "539142857"}); err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	deadline := time.Now().Add(time.Second)
	for {
		status, err := f.Status()
		if err != nil {
			t.Fatal(err)
		}
		if status.Locked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tune did not acquire before OpenStream")
		}
		time.Sleep(time.Millisecond)
	}
	if err := f.StartStream(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	buf := make([]byte, 188)
	n, err := f.Read(ctx, buf)
	if err != nil || n != 188 || buf[0] != 0x47 {
		t.Fatalf("read %d %v", n, err)
	}
	if err := f.Release(); err != nil {
		t.Fatal(err)
	}
	status, _ := f.Status()
	if status.Locked {
		t.Fatal("release retained lock")
	}
	if !b.stopped {
		t.Fatal("release leaked hardware")
	}
}
func TestTunePropagatesInitializationFailure(t *testing.T) {
	sentinel := errors.New("device failed")
	b := &fakeBridge{closed: make(chan struct{}), fail: sentinel}
	f := newFrontend(b, b, func() (processor, error) { return &fakeProcessor{}, nil })
	if err := f.Tune(map[string]string{"DELIVERY_SYSTEM": "ISDBT", "FREQUENCY": "539142857"}); !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}
}
func TestLayoutHasOneISDBTFrontendWithoutCard(t *testing.T) {
	layout := Layout(usbio.ID{Vendor: 0x1df7, Product: 0x2500})
	if len(layout) != 1 || !layout[0].Supports("ISDBT", func(s string) string {
		if s == "ISDB-T" {
			return "ISDBT"
		}
		return s
	}) {
		t.Fatalf("layout %+v", layout)
	}
	var _ device.TunerInfo = layout[0]
}

func TestIdleFrontendCannotStreamAndTunedStreamCanReopen(t *testing.T) {
	b := &fakeBridge{closed: make(chan struct{})}
	f := newFrontend(b, b, func() (processor, error) { return &fakeProcessor{}, nil })
	if err := f.StartStream(); err == nil {
		t.Fatal("idle frontend accepted stream")
	}
	if err := f.Tune(map[string]string{"DELIVERY_SYSTEM": "ISDBT", "FREQUENCY": "539142857"}); err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	f.StopStream()
	if err := f.StartStream(); err != nil {
		t.Fatalf("reopen stream: %v", err)
	}
}
func TestStaleStatusClearsLockNames(t *testing.T) {
	f := newFrontend(nil, nil, nil)
	f.status = device.Status{Locked: true, Names: append([]string(nil), device.LockNames...)}
	f.lastIQ = time.Now().Add(-2 * time.Second)
	s, _ := f.Status()
	if s.Locked || len(s.Names) > 0 {
		t.Fatalf("stale status %+v", s)
	}
}
