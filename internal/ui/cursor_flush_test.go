package ui

import (
	"bytes"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// drawableFunc adapts a closure to uv.Drawable.
type drawableFunc func(scr uv.Screen, rect uv.Rectangle)

func (f drawableFunc) Draw(scr uv.Screen, rect uv.Rectangle) { f(scr, rect) }

// TestCursorMoveEmittedSameFrame guards the vendored ultraviolet patch in
// TerminalScreen.Flush: without the added rend.Flush() drain, a cursor move
// requested by SetCursorPosition is held in the renderer's internal buffer and
// only emitted with the NEXT frame's diff. That left the visible cursor one
// frame behind the model — after a backspace it sat one column right of the
// text end, and after a focus switch it stayed in the old pane.
//
// If this test fails, the vendor tree was probably regenerated (go mod vendor)
// and the patch in vendor/github.com/charmbracelet/ultraviolet/terminal_screen.go
// was lost. Re-apply it: in Flush, after s.rend.MoveTo(...), add
// _ = s.rend.Flush().
func TestCursorMoveEmittedSameFrame(t *testing.T) {
	var out bytes.Buffer
	scr := uv.NewTerminalScreen(&out, uv.Environ{})
	scr.Resize(20, 5)
	scr.EnterAltScreen()

	draw := drawableFunc(func(s uv.Screen, rect uv.Rectangle) {
		c := uv.NewCell(s.WidthMethod(), "hi")
		s.SetCell(0, 0, c)
	})

	// Frame 1: draw content, cursor at (0,1).
	scr.SetCursorPosition(0, 1)
	scr.ShowCursor()
	if err := scr.Display(draw); err != nil {
		t.Fatal(err)
	}
	out.Reset()

	// Frame 2: content unchanged, move the cursor to (3,1). The move must be
	// part of this frame's output; unpatched uv defers it to the next flush.
	scr.SetCursorPosition(3, 1)
	scr.ShowCursor()
	if err := scr.Display(draw); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	want := "\x1b[2;4H" // CUP row 2, col 4 (1-based) == (3,1)
	if !strings.Contains(got, want) {
		t.Fatalf("cursor move %q not emitted in the same frame; output: %q", want, got)
	}
}