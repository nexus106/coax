// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package pool

import (
	"errors"
	"sync"
	"testing"

	"github.com/Till0196/coax/internal/coaxd/device"
	"github.com/Till0196/coax/pkg/usbio"
)

func newResetPool(t *testing.T, options Options) (*Supervisor, *fakeRegistry, *fakeBoard) {
	t.Helper()
	tuners := []device.TunerInfo{{Index: 0, Deliveries: []string{"ISDBT"}}}
	b := &fakeBoard{tuners: tuners}
	r := &fakeRegistry{detected: []Detected{{Backend: "msi2500", Selector: "001:007", Location: "001:007", Port: "1-2", USB: usbio.ID{Vendor: 0x1df7, Product: 0x2500}, Deliveries: []string{"ISDBT"}, Tuners: tuners}}, boards: map[string]*fakeBoard{"001:007": b}}
	s := New(r, options)
	t.Cleanup(func() { s.Close() })
	return s, r, b
}
func resetAddress(r *fakeRegistry, index int, selector string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	old := r.detected[index].Selector
	r.detected[index].Selector = selector
	r.detected[index].Location = selector
	r.boards[selector] = r.boards[old]
	delete(r.boards, old)
}
func resetLease(t *testing.T, s *Supervisor) *Lease {
	t.Helper()
	l, e := s.Lease(Want{DeliverySystem: "ISDBT", Owner: s.NewOwner()})
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func assertLeaseAlive(t *testing.T, l *Lease) {
	t.Helper()
	select {
	case <-l.Done():
		t.Fatal("same-port address change expired the live lease")
	default:
	}
}
func TestMSI2500AddressChangePreservesLeaseAndMeasurement(t *testing.T) {
	s, r, _ := newResetPool(t, Options{Linger: LingerForever})
	l := resetLease(t, s)
	resetAddress(r, 0, "001:008")
	boards, e := s.Boards()
	if e != nil {
		t.Fatal(e)
	}
	assertLeaseAlive(t, l)
	if !boards[0].Tuners[0].Leased || boards[0].Tuners[0].ID != l.ID() {
		t.Fatal("status lost the held tuner")
	}
	if reading, ok := s.Measure(l.ID()); !ok || !reading.Status.Locked {
		t.Fatal("status cannot measure held tuner after reset")
	}
}
func TestMSI2500ReopensLatestAddress(t *testing.T) {
	s, r, _ := newResetPool(t, Options{})
	l := resetLease(t, s)
	resetAddress(r, 0, "001:008")
	if _, e := s.Boards(); e != nil {
		t.Fatal(e)
	}
	l.Release()
	next := resetLease(t, s)
	defer next.Release()
	if r.openCount() != 2 {
		t.Fatal("next generation did not reopen hardware")
	}
}
func TestMSI2500AbsenceAndReplacementExpireLease(t *testing.T) {
	for _, replacement := range []string{"absent", "different USB ID"} {
		t.Run(replacement, func(t *testing.T) {
			s, r, _ := newResetPool(t, Options{Linger: LingerForever})
			l := resetLease(t, s)
			r.mutex.Lock()
			if replacement == "absent" {
				r.detected = nil
			} else {
				r.detected[0].USB.Product = 0x2501
			}
			r.mutex.Unlock()
			if _, e := s.Boards(); e != nil {
				t.Fatal(e)
			}
			select {
			case <-l.Done():
			default:
				t.Fatal("missing or replaced physical hardware retained old lease")
			}
		})
	}
}
func TestMSI2500AddressChangesKeepTwoPortsSeparate(t *testing.T) {
	s, r, a := newResetPool(t, Options{Linger: LingerForever})
	b := &fakeBoard{tuners: a.tuners}
	r.mutex.Lock()
	other := r.detected[0]
	other.Port = "1-3"
	other.Selector = "001:009"
	other.Location = "001:009"
	r.detected = append(r.detected, other)
	r.boards["001:009"] = b
	r.mutex.Unlock()
	first := resetLease(t, s)
	second := resetLease(t, s)
	resetAddress(r, 0, "001:010")
	if _, e := s.Boards(); e != nil {
		t.Fatal(e)
	}
	assertLeaseAlive(t, first)
	assertLeaseAlive(t, second)
	if first.ID() == second.ID() {
		t.Fatal("distinct physical ports share tuner identity")
	}
	r.mutex.Lock()
	r.detected = r.detected[1:]
	r.mutex.Unlock()
	if _, e := s.Boards(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-first.Done():
	default:
		t.Fatal("removed first device retained lease")
	}
	assertLeaseAlive(t, second)
}
func TestMSI2500CardSourceUsesCurrentAddress(t *testing.T) {
	s, r, b := newResetPool(t, Options{Linger: LingerForever})
	b.card = true
	l := resetLease(t, s)
	resetAddress(r, 0, "001:008")
	borrow, e := s.Cards().Open("msi2500", "001:008")
	if e != nil {
		t.Fatal(e)
	}
	defer borrow.Release()
	assertLeaseAlive(t, l)
	if r.openCount() != 1 {
		t.Fatal("card lookup opened a second actor")
	}
	if _, e = s.Cards().Open("msi2500", "001:007"); !errors.Is(e, ErrUnknownTuner) {
		t.Fatalf("old selector still accepted: %v", e)
	}
}
func TestMSI2500ConcurrentRefreshAndReopen(t *testing.T) {
	s, r, _ := newResetPool(t, Options{})
	l := resetLease(t, s)
	resetAddress(r, 0, "001:008")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				if _, e := s.Boards(); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	assertLeaseAlive(t, l)
	l.Release()
	for i := 0; i < 30; i++ {
		next := resetLease(t, s)
		next.Release()
	}
	wg.Wait()
}
func TestOtherBackendAddressChangeExpiresLease(t *testing.T) {
	s, r, _ := newResetPool(t, Options{Linger: LingerForever})
	r.detected[0].Backend = "other"
	l := resetLease(t, s)
	resetAddress(r, 0, "001:008")
	if _, e := s.Boards(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-l.Done():
	default:
		t.Fatal("unrelated backend gained implicit re-enumeration persistence")
	}
}

// A firmware-reset open is bounded by the backend and can temporarily remove
// its own device from enumeration before returning the reopened handle.
type resetOpeningRegistry struct {
	*fakeRegistry
	entered chan struct{}
	resume  chan struct{}
}

func (r *resetOpeningRegistry) Open(one Detected, options device.Options) (device.Board, error) {
	close(r.entered)
	<-r.resume
	return r.fakeRegistry.Open(one, options)
}
func TestMSI2500OpeningSurvivesTemporaryEnumerationAbsence(t *testing.T) {
	_, r, _ := newResetPool(t, Options{Linger: LingerForever})
	blocked := &resetOpeningRegistry{fakeRegistry: r, entered: make(chan struct{}), resume: make(chan struct{})}
	s := New(blocked, Options{Linger: LingerForever})
	defer s.Close()
	type result struct {
		lease *Lease
		err   error
	}
	done := make(chan result, 1)
	go func() { l, e := s.Lease(Want{DeliverySystem: "ISDBT", Owner: s.NewOwner()}); done <- result{l, e} }()
	<-blocked.entered
	r.mutex.Lock()
	original := r.detected[0]
	r.detected = nil
	r.mutex.Unlock()
	if _, e := s.Boards(); e != nil {
		t.Fatal(e)
	}
	r.mutex.Lock()
	replacement := original
	replacement.Selector = "001:008"
	replacement.Location = "001:008"
	r.detected = []Detected{replacement}
	r.boards["001:008"] = r.boards["001:007"]
	r.mutex.Unlock()
	if _, e := s.Boards(); e != nil {
		t.Fatal(e)
	}
	close(blocked.resume)
	acquired := <-done
	if acquired.err != nil {
		t.Fatalf("self-reset opener was retired: %v", acquired.err)
	}
	assertLeaseAlive(t, acquired.lease)
	if _, ok := s.Measure(acquired.lease.ID()); !ok {
		t.Fatal("re-enumerated opener cannot be measured")
	}
}

func TestMSI2500OpeningDoesNotHideDifferentProductAtSamePort(t *testing.T) {
	_, r, _ := newResetPool(t, Options{Linger: LingerForever})
	blocked := &resetOpeningRegistry{fakeRegistry: r, entered: make(chan struct{}), resume: make(chan struct{})}
	s := New(blocked, Options{Linger: LingerForever})
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, e := s.Lease(Want{DeliverySystem: "ISDBT", Owner: s.NewOwner()}); done <- e }()
	<-blocked.entered
	r.mutex.Lock()
	r.detected[0].USB.Product = 0x2501
	r.mutex.Unlock()
	if _, e := s.Boards(); e != nil {
		t.Fatal(e)
	}
	close(blocked.resume)
	if e := <-done; e == nil {
		t.Fatal("different USB product retained the old in-flight actor")
	}
}
