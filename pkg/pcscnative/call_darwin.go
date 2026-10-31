// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

//go:build darwin

// Native C calls use the same libSystem dynamic-import and pinned-memory
// technique as pkg/usbio/iokit. No C code or external PC/SC library is linked.

package pcscnative

import (
	"runtime"
	"syscall"
	"unsafe"
)

//go:cgo_import_dynamic libc_dlopen dlopen "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_dlsym dlsym "/usr/lib/libSystem.B.dylib"

// The imported functions are named as data so their addresses can be taken: the
// loader fills the symbol in with the real function's address, so &variable is
// the pointer to call. The variables are never read -- reading one would read
// the first byte of the function's code.

//go:linkname libc_dlopen libc_dlopen
var libc_dlopen byte

//go:linkname libc_dlsym libc_dlsym
var libc_dlsym byte

func symbol(imported *byte) uintptr { return uintptr(unsafe.Pointer(imported)) }

// The standard library's bridge from Go to a C function pointer. It puts the goroutine into
// a system call, so a blocking transfer does not hold up the scheduler.
//
//go:linkname syscallDarwin syscall.syscall
func syscallDarwin(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname syscallDarwin6 syscall.syscall6
func syscallDarwin6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname syscallDarwin9 syscall.syscall9
func syscallDarwin9(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (r1, r2 uintptr, err syscall.Errno)

// pin moves the referenced object to the heap and keeps it fixed for C to
// access. Go does not otherwise keep an object alive through a uintptr.
func pin(values ...any) *runtime.Pinner {
	pinner := new(runtime.Pinner)
	for _, value := range values {
		pinner.Pin(value)
	}
	return pinner
}

// call invokes a C function pointer. Every entry point used here takes only
// integers and pointers. Every Go pointer argument must be held with pin.
func call(fn uintptr, args ...uintptr) uintptr {
	var padded [9]uintptr
	copy(padded[:], args)
	switch {
	case len(args) <= 3:
		result, _, _ := syscallDarwin(fn, padded[0], padded[1], padded[2])
		return result
	case len(args) <= 6:
		result, _, _ := syscallDarwin6(fn, padded[0], padded[1], padded[2], padded[3], padded[4], padded[5])
		return result
	default:
		result, _, _ := syscallDarwin9(fn, padded[0], padded[1], padded[2], padded[3],
			padded[4], padded[5], padded[6], padded[7], padded[8])
		return result
	}
}

// The dlopen mode: resolve everything now, and keep the symbols private.
const rtldNow, rtldLocal = 0x2, 0x4

// dlopen opens a shared library by name, returning zero when it is not there.
func dlopen(name string) uintptr {
	bytes := append([]byte(name), 0)
	defer pin(&bytes[0]).Unpin()
	handle, _, _ := syscallDarwin(symbol(&libc_dlopen),
		uintptr(unsafe.Pointer(&bytes[0])), rtldNow|rtldLocal, 0)
	return handle
}

func dlsym(handle uintptr, name string) uintptr {
	bytes := append([]byte(name), 0)
	defer pin(&bytes[0]).Unpin()
	address, _, _ := syscallDarwin(symbol(&libc_dlsym),
		handle, uintptr(unsafe.Pointer(&bytes[0])), 0)
	return address
}
