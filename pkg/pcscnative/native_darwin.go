//go:build darwin

// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package pcscnative

import (
	"context"
	"fmt"
	"github.com/Till0196/coax/pkg/winscard"
	"sync"
	"unsafe"
)

var library struct {
	sync.Once
	handle                                                  uintptr
	establish, release, list, connect, disconnect, transmit uintptr
	err                                                     error
}

func initLibrary() error {
	library.Do(func() {
		library.handle = dlopen("/System/Library/Frameworks/PCSC.framework/PCSC")
		if library.handle == 0 {
			library.err = fmt.Errorf("load macOS PC/SC framework")
			return
		}
		for _, s := range []struct {
			name string
			dst  *uintptr
		}{{"SCardEstablishContext", &library.establish}, {"SCardReleaseContext", &library.release}, {"SCardListReaders", &library.list}, {"SCardConnect", &library.connect}, {"SCardDisconnect", &library.disconnect}, {"SCardTransmit", &library.transmit}} {
			*s.dst = dlsym(library.handle, s.name)
			if *s.dst == 0 {
				library.err = fmt.Errorf("PC/SC entry point %s unavailable", s.name)
			}
		}
	})
	return library.err
}

type connection struct {
	mu                      sync.Mutex
	context, card, protocol uint32
	closed                  bool
}

func open(ctx context.Context, name string) (Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := initLibrary(); err != nil {
		return nil, err
	}
	c := new(connection)
	defer pin(&c.context, &c.card, &c.protocol).Unpin()
	if err := status(call(library.establish, 2, 0, 0, uintptr(unsafe.Pointer(&c.context)))); err != nil {
		return nil, fmt.Errorf("establish PC/SC: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			call(library.release, uintptr(c.context))
		}
	}()
	n := new(uint32)
	defer pin(n).Unpin()
	if err := status(call(library.list, uintptr(c.context), 0, 0, uintptr(unsafe.Pointer(n)))); err != nil {
		return nil, fmt.Errorf("list card readers: %w", err)
	}
	if *n == 0 || *n > 65536 {
		return nil, fmt.Errorf("no PC/SC card readers")
	}
	names := make([]byte, *n)
	defer pin(&names[0]).Unpin()
	if err := status(call(library.list, uintptr(c.context), 0, uintptr(unsafe.Pointer(&names[0])), uintptr(unsafe.Pointer(n)))); err != nil {
		return nil, err
	}
	var readers []string
	start := 0
	for i, b := range names {
		if b == 0 {
			if i == start {
				break
			}
			readers = append(readers, string(names[start:i]))
			start = i + 1
		}
	}
	if name == "" {
		if len(readers) != 1 {
			return nil, fmt.Errorf("select --card-reader: found %d readers", len(readers))
		}
		name = readers[0]
	}
	found := false
	for _, r := range readers {
		if r == name {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("card reader %q not found", name)
	}
	z := append([]byte(name), 0)
	defer pin(&z[0]).Unpin()
	if err := status(call(library.connect, uintptr(c.context), uintptr(unsafe.Pointer(&z[0])), uintptr(winscard.ShareExclusive), uintptr(winscard.ProtocolT1), uintptr(unsafe.Pointer(&c.card)), uintptr(unsafe.Pointer(&c.protocol)))); err != nil {
		return nil, fmt.Errorf("connect card: %w", err)
	}
	ok = true
	return c, nil
}
func (c *connection) Transmit(ctx context.Context, command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.closed {
		return nil, winscard.ErrRemovedCard
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("empty card command")
	}
	request := &struct{ Protocol, Length uint32 }{c.protocol, 8}
	n := new(uint32)
	*n = 4096
	response := make([]byte, *n)
	defer pin(request, n, &command[0], &response[0]).Unpin()
	err := status(call(library.transmit, uintptr(c.card), uintptr(unsafe.Pointer(request)), uintptr(unsafe.Pointer(&command[0])), uintptr(len(command)), 0, uintptr(unsafe.Pointer(&response[0])), uintptr(unsafe.Pointer(n))))
	if err != nil {
		return nil, fmt.Errorf("card exchange: %w", err)
	}
	if *n > uint32(len(response)) {
		return nil, fmt.Errorf("invalid PC/SC response size")
	}
	return response[:*n], ctx.Err()
}
func (c *connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	err := status(call(library.disconnect, uintptr(c.card), 0))
	releaseErr := status(call(library.release, uintptr(c.context)))
	if err != nil {
		return err
	}
	return releaseErr
}
