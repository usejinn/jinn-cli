package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
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
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		cols, rows = 80, 24
	}
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
	resized := make(chan struct{}, 1)
	stopResize := notifyResize(resized)
	defer stopResize()
	go func() {
		for range resized {
			if cols, rows, err := term.GetSize(fd); err == nil {
				size := make([]byte, 4)
				binary.BigEndian.PutUint16(size, uint16(rows))
				binary.BigEndian.PutUint16(size[2:], uint16(cols))
				_ = send(jinn.FrameResize, size)
			}
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
