package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"sync"

	"github.com/coder/websocket"
	"golang.org/x/term"
	jinn "usejinn.com/go"
)

// shell opens a terminal in a run: a login shell as root in /workspace,
// through Jinn's relay, sealed end to end (OpenTerminal). It exits with
// the shell's status.
func shell(ctx context.Context, args []string) error {
	if len(args) != 1 || !jinnRun(args[0]) {
		return usageError("usage: jinn shell run_…")
	}
	if asJSON {
		return usageError("jinn shell is interactive; use jinn exec for JSON")
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return usageError("jinn shell needs a terminal; use jinn exec for scripts")
	}
	c, err := client()
	if err != nil {
		return err
	}
	rows, cols := termSize()
	ticket, ch, err := c.OpenTerminal(ctx, args[0], rows, cols, nil)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, ticket.RelayURL+"?route="+ticket.Route+"&side=caller", nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, old)
	var mu sync.Mutex // frames are sealed in order, one at a time
	send := func(kind byte, data []byte) error {
		mu.Lock()
		defer mu.Unlock()
		return conn.Write(ctx, websocket.MessageBinary, ch.Seal(kind, data))
	}
	// Each change of size goes to the shell, as a resize frame.
	resized := make(chan struct{}, 1)
	stopResize := notifyResize(resized)
	defer stopResize()
	go func() {
		for range resized {
			r, c := termSize()
			if r == rows && c == cols {
				continue
			}
			rows, cols = r, c
			size := make([]byte, 4)
			binary.BigEndian.PutUint16(size, uint16(rows))
			binary.BigEndian.PutUint16(size[2:], uint16(cols))
			_ = send(jinn.FrameResize, size)
		}
	}()
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 && send(jinn.FrameInput, buf[:n]) != nil {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return errors.New("the terminal closed before its shell exited")
		}
		kind, data, err := ch.Open(msg)
		if err != nil {
			return err
		}
		switch kind {
		case jinn.FrameOutput:
			_, _ = os.Stdout.Write(data)
		case jinn.FrameExit:
			if code := int32(binary.BigEndian.Uint32(data)); code != 0 {
				term.Restore(fd, old)
				return exitStatus(code)
			}
			return nil
		}
	}
}

func jinnRun(s string) bool { return len(s) == 28 && s[:4] == "run_" }

// termSize is the terminal's size, as the run's shell gets it: stdin's, else
// stdout's, else $LINES and $COLUMNS, else 24 by 80. A terminal command takes
// at most 1,000 a side.
func termSize() (rows, cols int) {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		if c, r, err := term.GetSize(int(f.Fd())); err == nil && r > 0 && c > 0 {
			return min(r, 1000), min(c, 1000)
		}
	}
	r, _ := strconv.Atoi(os.Getenv("LINES"))
	c, _ := strconv.Atoi(os.Getenv("COLUMNS"))
	if r > 0 && c > 0 {
		return min(r, 1000), min(c, 1000)
	}
	return 24, 80
}
