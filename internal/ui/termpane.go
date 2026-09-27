package ui

import (
	"strings"

	"github.com/justin/conterm/internal/shell"

	uv "github.com/charmbracelet/ultraviolet"
	vt "github.com/charmbracelet/x/vt"
)

// selState is a mouse selection over the terminal's virtual line space
// (scrollback lines first, then screen rows).
type selState struct {
	dragging bool // mouse button held
	have     bool // a selection exists (may be finalized)
	v0, v1   int  // anchor and head virtual line indices
	x0, x1   int  // columns at those lines
}

// termLineText returns the text of virtual line v (scrollback first,
// then screen rows), holding the emulator lock. Caller must hold it.
func (a *App) termLineText(v *termView, vl int) string {
	var b strings.Builder
	w := v.width
	for x := 0; x < w; x++ {
		var cell *uv.Cell
		if vl < v.sbLen {
			cell = v.term.ScrollbackCellAt(x, vl)
		} else {
			cell = v.term.CellAt(x, vl-v.sbLen)
		}
		if cell == nil || cell.Content == "" {
			continue
		}
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " \x00")
}

// termView snapshots the dimensions needed to render the terminal pane.
// The shell I/O lock is held between termSnapshot and done.
type termView struct {
	sh     *shell.Shell
	term   *vt.SafeEmulator
	sbLen  int
	width  int
	hTotal int // sbLen + screen height
}

func (a *App) termSnapshot() *termView {
	v := &termView{sh: a.shell}
	v.sh.Lock()
	v.term = v.sh.Terminal()
	v.sbLen = v.term.ScrollbackLen()
	v.width = v.term.Width()
	v.hTotal = v.sbLen + v.term.Height()
	return v
}

func (v *termView) done() { v.sh.Unlock() }

// selectionText extracts the selected text under the emulator lock.
func (a *App) selectionText() string {
	if !a.sel.have {
		return ""
	}
	v := a.termSnapshot()
	defer v.done()
	return a.selectionTextLocked(v)
}

func (a *App) selectionTextLocked(v *termView) string {
	lo, hi := a.sel.v0, a.sel.v1
	xlo, xhi := a.sel.x0, a.sel.x1
	if lo > hi {
		lo, hi = hi, lo
		xlo, xhi = xhi, xlo
	}
	if lo < 0 {
		lo = 0
	}
	if hi >= v.hTotal {
		hi = v.hTotal - 1
	}
	var lines []string
	for vl := lo; vl <= hi; vl++ {
		text := a.termLineText(v, vl)
		runes := []rune(text)
		if vl == lo || vl == hi {
			// clip to the column range on the edge lines
			c0, c1 := 0, len(runes)
			if vl == lo {
				c0 = min(xlo, len(runes))
			}
			if vl == hi {
				c1 = min(xhi+1, len(runes))
			}
			if c1 <= c0 {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, strings.TrimRight(string(runes[c0:c1]), " "))
			continue
		}
		lines = append(lines, text)
	}
	// drop blank lines at the edges
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// inSelection reports whether column x of virtual line vl is selected.
func (a *App) inSelection(vl, x int) bool {
	if !a.sel.have {
		return false
	}
	lo, hi := a.sel.v0, a.sel.v1
	xlo, xhi := a.sel.x0, a.sel.x1
	if lo > hi {
		lo, hi = hi, lo
		xlo, xhi = xhi, xlo
	}
	if vl < lo || vl > hi {
		return false
	}
	if vl == lo && x < xlo {
		return false
	}
	if vl == hi && x > xhi {
		return false
	}
	return true
}

// drawTerminal renders the emulator state (or the scrolled-back view)
// into rect and reports the cursor position for the host to display.
func (a *App) drawTerminal(scr uv.Screen, rect uv.Rectangle) (cursor uv.Position, showCursor bool) {
	v := a.termSnapshot()
	defer v.done()
	t := v.term

	scroll := min(a.termScroll, v.sbLen)
	viewTop := v.sbLen - scroll

	// New output while following: termScroll only moves on explicit wheel.
	if a.termScroll > 0 && scroll < a.termScroll {
		a.termScroll = scroll
	}

	for y := 0; y < rect.Dy(); y++ {
		vl := viewTop + y
		if vl >= v.hTotal {
			break
		}
		for x := 0; x < rect.Dx() && x < v.width; x++ {
			var cell *uv.Cell
			if vl < v.sbLen {
				cell = t.ScrollbackCellAt(x, vl)
			} else {
				cell = t.CellAt(x, vl-v.sbLen)
			}
			if cell == nil || cell.Content == "" {
				continue
			}
			c := cell.Clone()
			if a.inSelection(vl, x) {
				c.Style.Attrs |= uv.AttrReverse
			}
			scr.SetCell(rect.Min.X+x, rect.Min.Y+y, c)
		}
	}

	if scroll == 0 {
		snap := a.shell.Snapshot()
		if snap.CursorVis {
			p := t.CursorPosition()
			if p.X < rect.Dx() && p.Y < rect.Dy() {
				return uv.Position{X: rect.Min.X + p.X, Y: rect.Min.Y + p.Y}, true
			}
		}
	}
	return uv.Position{}, false
}

// terminalTail returns the last n lines of terminal text (scrollback +
// screen), blank lines trimmed from the edges. Used as AI context.
func (a *App) terminalTail(n int) string {
	if a.shell == nil || n <= 0 {
		return ""
	}
	v := a.termSnapshot()
	defer v.done()
	start := max(0, v.hTotal-n)
	var lines []string
	for vl := start; vl < v.hTotal; vl++ {
		lines = append(lines, a.termLineText(v, vl))
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// vlineAt maps a pane row to its virtual line index for the current
// scroll position.
func (a *App) vlineAt(rect uv.Rectangle, y int) int {
	v := a.termSnapshot()
	defer v.done()
	return v.sbLen - min(a.termScroll, v.sbLen) + (y - rect.Min.Y)
}
