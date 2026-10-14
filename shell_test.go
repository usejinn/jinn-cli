package main

import (
	"os"
	"testing"

	"golang.org/x/term"
)

// Without a terminal to ask, the size comes from $LINES and $COLUMNS, at
// most 1,000 a side, else 24 by 80.
func TestTermSizeWithoutATerminal(t *testing.T) {
	if term.IsTerminal(int(os.Stdin.Fd())) || term.IsTerminal(int(os.Stdout.Fd())) {
		t.Skip("stdin or stdout is a terminal")
	}
	t.Setenv("LINES", "50")
	t.Setenv("COLUMNS", "5000")
	if r, c := termSize(); r != 50 || c != 1000 {
		t.Fatalf("from the environment: %d by %d", r, c)
	}
	t.Setenv("COLUMNS", "")
	if r, c := termSize(); r != 24 || c != 80 {
		t.Fatalf("with nothing to go on: %d by %d", r, c)
	}
}
