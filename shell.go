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

// shell opens a terminal in a run or running machine: a login shell as root in
// /workspace, through Jinn's SSH relay. It exits with the shell's status.
func shell(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] == "" {
		return usageError("usage: jinn shell run_…|MACHINE")
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
	var ticket jinn.TerminalTicket
	if jinnRun(args[0]) {
		ticket, err = c.OpenTerminal(ctx, args[0])
	} else {
		ticket, err = c.OpenMachineTerminal(ctx, args[0])
	}
	if err != nil {
		return err
	}
	rows, cols := termSize()
	conn, _, err := websocket.Dial(ctx, ticket.URL(rows, cols), nil)
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
	var mu sync.Mutex
	writeFrame := func(frame []byte) error {
		mu.Lock()
		defer mu.Unlock()
		return conn.Write(ctx, websocket.MessageBinary, frame)
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
			_ = writeFrame(jinn.EncodeResizeFrame(rows, cols))
		}
	}()
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 && writeFrame(jinn.EncodeTerminalFrame(jinn.FrameInput, buf[:n])) != nil {
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
		kind, data, err := jinn.DecodeTerminalFrame(msg)
		if err != nil {
			return err
		}
		switch kind {
		case jinn.FrameOutput:
			_, _ = os.Stdout.Write(data)
		case jinn.FrameExit:
			if len(data) >= 4 {
				if code := int32(binary.BigEndian.Uint32(data)); code != 0 {
					term.Restore(fd, old)
					return exitStatus(code)
				}
			}
			return nil
		}
	}
}

func jinnRun(s string) bool { return len(s) == 28 && s[:4] == "run_" }

// termSize is the terminal's size, as the run's shell gets it: stdin's, else
// stdout's, else $LINES and $COLUMNS, else 24 by 80. A terminal takes at most
// 1,000 a side.
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
