// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

// Package msi2500 connects the Go software demodulator to the existing tuner host.
package msi2500

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	demod "github.com/Till0196/coax/internal/coaxd/demod/msi2500"
	"github.com/Till0196/coax/internal/coaxd/device"
	"github.com/Till0196/coax/internal/coaxd/tuning"
	"github.com/Till0196/coax/internal/coaxd/usb/boardkit"
	bridge "github.com/Till0196/coax/internal/coaxd/usb/bridge/msi2500"
	"github.com/Till0196/coax/internal/coaxd/usb/usbdev"
	"github.com/Till0196/coax/pkg/usbio"
)

var ids = []usbio.ID{{Vendor: 0x1df7, Product: 0x2500}}
var deliveries = []string{"ISDB-T"}
var Backend = usbdev.Backend{
	Name:        "msi2500",
	Aliases:     []string{"dtv02-1t-u"},
	IDs:         ids,
	Description: "DTV02-1T-U (MSi2500 + MSi001 software ISDB-T)",
	Deliveries:  deliveries,
	Supports:    Supports,
	Layout:      Layout,
	Open:        open,
}

func Supports(system string) bool { return tuning.Normalize(system) == "ISDBT" }
func gainSteps() []float64 {
	g := make([]float64, 86)
	for i := range g {
		g[i] = float64(i)
	}
	return g
}
func Layout(usbio.ID) []device.TunerInfo {
	return []device.TunerInfo{{Index: 0, Name: "DTV02-1T-U", Deliveries: append([]string(nil), deliveries...), Controls: device.Controls{Gain: &device.GainControl{Decibels: gainSteps()}}}}
}
func open(options device.Options) (device.Board, error) {
	t, err := usbio.Open(options.Selector, ids)
	if err != nil {
		return nil, err
	}
	b, err := bridge.New(t)
	if err != nil {
		t.Close()
		return nil, err
	}
	board, err := boardkit.New(boardkit.Config{Model: "DTV02-1T-U", Location: t.Name(), Tuners: Layout(ids[0]), Release: b.Close, Supports: Supports, Open: func(int) (boardkit.Tuner, error) {
		return newFrontend(b, func() (processor, error) { return demod.New(demod.Config{}) }), nil
	}})
	if err != nil {
		b.Close()
	}
	return board, err
}

type iqBridge interface {
	Tune(uint32, int) error
	Start() error
	Stop() error
	ReadIQ(context.Context, []complex64) (int, bool, error)
}
type processor interface {
	PushIQ([]complex64, bool) ([]byte, error)
	Locked() bool
	Snapshot() demod.SyncSnapshot
}
type iqBlock struct {
	samples []complex64
	n       int
	loss    bool
	err     error
}

type frontend struct {
	bridge    iqBridge
	factory   func() (processor, error)
	life      sync.Mutex
	mu        sync.Mutex
	gain      int
	cancel    context.CancelFunc
	done      chan struct{}
	status    device.Status
	lastIQ    time.Time
	failure   error
	streaming bool
	queue     [][]byte
	leftover  []byte
	dropped   uint64
	notify    chan struct{}
}

func newFrontend(b iqBridge, factory func() (processor, error)) *frontend {
	return &frontend{bridge: b, factory: factory, gain: 15, notify: make(chan struct{}, 1)}
}
func (f *frontend) GainLadder(string) tuning.GainLadder { return tuning.NewGainLadder(gainSteps()...) }
func (f *frontend) SetGain(step int) error {
	if step < 0 || step > 85 {
		return errors.New("MSi001 gain must be 0..85 dB")
	}
	f.mu.Lock()
	f.gain = step
	f.mu.Unlock()
	return nil
}
func (f *frontend) Tune(properties map[string]string) error {
	if !Supports(properties["DELIVERY_SYSTEM"]) {
		return fmt.Errorf("DTV02-1T-U does not support %q", properties["DELIVERY_SYSTEM"])
	}
	hz, err := tuning.Number(properties, "FREQUENCY", true)
	if err != nil {
		return err
	}
	if hz < 1_000_000 {
		hz *= 1000
	}
	if hz < 330000000 || hz >= 960000000 {
		return fmt.Errorf("%w: %d Hz", device.ErrOutOfRange, hz)
	}
	f.life.Lock()
	defer f.life.Unlock()
	f.stopLocked()
	p, err := f.factory()
	if err != nil {
		return err
	}
	f.mu.Lock()
	gain := f.gain
	f.mu.Unlock()
	if err := f.bridge.Tune(uint32(hz), gain); err != nil {
		return err
	}
	if err := f.bridge.Start(); err != nil {
		f.bridge.Stop()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	f.cancel = cancel
	f.done = done
	f.mu.Lock()
	f.failure = nil
	f.queue = nil
	f.leftover = nil
	f.streaming = false
	f.status = device.Status{}
	f.lastIQ = time.Time{}
	f.dropped = 0
	f.mu.Unlock()
	go f.run(ctx, cancel, p, done)
	return nil
}
func (f *frontend) signal() {
	select {
	case f.notify <- struct{}{}:
	default:
	}
}
func (f *frontend) run(ctx context.Context, cancel context.CancelFunc, p processor, done chan struct{}) {
	events := make(chan *iqBlock, 16)
	free := make(chan *iqBlock, 16)
	for i := 0; i < 16; i++ {
		free <- &iqBlock{samples: make([]complex64, 32768)}
	}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(events)
		for {
			var b *iqBlock
			select {
			case <-ctx.Done():
				return
			case b = <-free:
			}
			n, loss, err := f.bridge.ReadIQ(ctx, b.samples)
			b.n = n
			b.loss = loss
			b.err = err
			select {
			case <-ctx.Done():
				return
			case events <- b:
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		cancel()
		<-readDone
		f.mu.Lock()
		f.status.Locked = false
		f.mu.Unlock()
		f.signal()
		close(done)
	}()
	for {
		var b *iqBlock
		var ok bool
		select {
		case <-ctx.Done():
			return
		case b, ok = <-events:
		}
		if !ok {
			return
		}
		if b.err != nil {
			if !errors.Is(b.err, context.Canceled) {
				f.mu.Lock()
				f.failure = b.err
				f.mu.Unlock()
			}
			return
		}
		packets, err := p.PushIQ(b.samples[:b.n], b.loss)
		free <- b
		if err != nil {
			f.mu.Lock()
			f.failure = err
			f.mu.Unlock()
			return
		}
		f.mu.Lock()
		f.lastIQ = time.Now()
		f.status = device.Status{Locked: p.Locked()}
		snapshot := p.Snapshot()
		if snapshot.Mode > 0 {
			f.status.Names = []string{"SIGNAL", "CARRIER"}
		}
		if f.status.Locked {
			f.status.Names = append([]string(nil), device.LockNames...)
		}
		if len(packets) > 0 {
			// Keep an acquisition window before the client opens its stream. During
			// streaming, the same bounded queue accounts explicitly for slow readers.
			limit := 64
			if !f.streaming {
				limit = 8
			}
			if len(f.queue) >= limit {
				if f.streaming {
					f.dropped += uint64(len(f.queue[0]) / 188)
				}
				copy(f.queue, f.queue[1:])
				f.queue = f.queue[:len(f.queue)-1]
			}
			f.queue = append(f.queue, append([]byte(nil), packets...))
		}
		f.mu.Unlock()
		f.signal()
	}
}
func (f *frontend) Status() (device.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.status
	s.Names = append([]string(nil), s.Names...)
	if f.lastIQ.IsZero() || time.Since(f.lastIQ) > time.Second {
		s.Locked = false
		s.Names = nil
	}
	return s, f.failure
}
func (f *frontend) StartStream() error {
	f.life.Lock()
	defer f.life.Unlock()
	if f.cancel == nil {
		return device.ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure != nil {
		return f.failure
	}
	f.streaming = true
	return nil
}
func (f *frontend) StopStream() error {
	f.life.Lock()
	defer f.life.Unlock()
	f.mu.Lock()
	f.streaming = false
	f.queue = nil
	f.leftover = nil
	f.mu.Unlock()
	f.signal()
	return nil
}
func (f *frontend) Release() error { f.life.Lock(); defer f.life.Unlock(); f.stopLocked(); return nil }
func (f *frontend) stopLocked() {
	if f.cancel != nil {
		f.cancel()
		f.bridge.Stop()
		<-f.done
		f.cancel = nil
		f.done = nil
	}
	f.mu.Lock()
	f.status = device.Status{}
	f.streaming = false
	f.queue = nil
	f.leftover = nil
	f.mu.Unlock()
	f.signal()
}
func (f *frontend) Dropped() uint64 { f.mu.Lock(); defer f.mu.Unlock(); return f.dropped }
func (f *frontend) Read(ctx context.Context, buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	for {
		f.mu.Lock()
		if len(f.leftover) == 0 && len(f.queue) > 0 {
			f.leftover = f.queue[0]
			f.queue[0] = nil
			f.queue = f.queue[1:]
		}
		if len(f.leftover) > 0 {
			n := copy(buffer, f.leftover)
			f.leftover = f.leftover[n:]
			f.mu.Unlock()
			return n, nil
		}
		failure, active := f.failure, f.streaming
		f.mu.Unlock()
		if failure != nil {
			return 0, failure
		}
		if !active {
			return 0, device.ErrClosed
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timer.C:
			return 0, device.ErrWouldBlock
		case <-f.notify:
		}
	}
}
