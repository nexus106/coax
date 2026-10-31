// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package cli

import (
	"context"
	"fmt"
	"github.com/Till0196/coax/pkg/b25"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func runDecode(arguments []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := newCommand("decode", stderr)
	input := command.flags.String("input", "", "scrambled TS file, or - for stdin")
	output := command.flags.String("output", "-", "clear TS file, or - for stdout")
	reader := command.flags.String("card-reader", "", "native PC/SC reader name (the only reader when omitted)")
	alias(command.flags, "o", "output")
	if err := command.parseOnly(arguments); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("--input is required")
	}
	if *input != "-" && *output != "-" {
		inPath, err := filepath.Abs(*input)
		if err != nil {
			return err
		}
		outPath, err := filepath.Abs(*output)
		if err != nil {
			return err
		}
		if inPath == outPath {
			return fmt.Errorf("input and output must be different files")
		}
		inInfo, inErr := os.Stat(*input)
		outInfo, outErr := os.Stat(*output)
		if inErr == nil && outErr == nil && os.SameFile(inInfo, outInfo) {
			return fmt.Errorf("input and output must be different files")
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	src := stdin
	if *input != "-" {
		f, err := os.Open(*input)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}
	card, err := b25.OpenCard(ctx, *reader)
	if err != nil {
		return err
	}
	defer card.Close()
	dst, closeDst, err := outputWriter(*output, stdout)
	if err != nil {
		return err
	}
	err = b25.Decode(ctx, dst, src, card)
	closeDst()
	return err
}
