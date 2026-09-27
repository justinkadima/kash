// Package ui implements the con terminal user interface on top of
// ultraviolet's screen and event model.
package ui

import (
	"image/color"
	"strconv"

	uv "github.com/charmbracelet/ultraviolet"
)

// ---- palette ----

var (
	stDim     = uv.Style{Fg: hexColor("#626a8c")}
	stAccent  = uv.Style{Fg: hexColor("#89b4fa")}
	stGreen   = uv.Style{Fg: hexColor("#a6e3a1")}
	stRed     = uv.Style{Fg: hexColor("#f38ba8")}
	stYellow  = uv.Style{Fg: hexColor("#f9e2af")}
	stPurple  = uv.Style{Fg: hexColor("#cba6f7")}
	stBorder  = uv.Style{Fg: hexColor("#454a6d")}
	stReverse = uv.Style{Attrs: uv.AttrReverse}
)

func hexColor(s string) color.Color {
	c := color.RGBA{}
	if len(s) == 7 && s[0] == '#' {
		if v, err := strconv.ParseUint(s[1:], 16, 32); err == nil {
			c.R = uint8(v >> 16)
			c.G = uint8(v >> 8)
			c.B = uint8(v)
			c.A = 255
		}
	}
	return c
}

// ---- geometry ----

func inRect(r uv.Rectangle, x, y int) bool {
	return x >= r.Min.X && x < r.Max.X && y >= r.Min.Y && y < r.Max.Y
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func absX(r uv.Rectangle, x int) int { return r.Min.X + x }
func absY(r uv.Rectangle, y int) int { return r.Min.Y + y }

// ---- cell drawing ----

// putStr writes s at (x, y) inside rect (coordinates relative to rect),
// clipped to the rect width. Returns the x position after the last cell.
func putStr(scr uv.Screen, rect uv.Rectangle, x, y int, s string, st uv.Style) int {
	m := scr.WidthMethod()
	for _, r := range s {
		gr := string(r)
		w := m.StringWidth(gr)
		if w <= 0 {
			continue // zero-width: combining marks etc.
		}
		if x+w > rect.Dx() {
			break
		}
		c := uv.NewCell(m, gr)
		c.Style = st
		scr.SetCell(absX(rect, x), absY(rect, y), c)
		x += w
	}
	return x
}

// fillRect paints the whole rect with styled spaces.
func fillRect(scr uv.Screen, rect uv.Rectangle, st uv.Style) {
	m := scr.WidthMethod()
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			c := uv.NewCell(m, " ")
			c.Style = st
			scr.SetCell(absX(rect, x), absY(rect, y), c)
		}
	}
}

// drawBox draws a rounded single-line box and returns the inner rect.
// title is drawn at the top edge after the corner.
func drawBox(scr uv.Screen, rect uv.Rectangle, st uv.Style, title string) uv.Rectangle {
	if rect.Dx() < 2 || rect.Dy() < 2 {
		return rect
	}
	w := rect.Dx() - 2
	top := "╭" + repeat("─", w) + "╮"
	bot := "╰" + repeat("─", w) + "╯"
	putStr(scr, rect, 0, 0, top, st)
	putStr(scr, rect, 0, rect.Dy()-1, bot, st)
	for y := 1; y < rect.Dy()-1; y++ {
		putStr(scr, rect, 0, y, "│", st)
		putStr(scr, rect, rect.Dx()-1, y, "│", st)
	}
	if title != "" && w > 4 {
		putStr(scr, rect, 2, 0, " "+title+" ", st)
	}
	return uv.Rect(rect.Min.X+1, rect.Min.Y+1, rect.Dx()-2, rect.Dy()-2)
}

func repeat(s string, n int) string {
	out := make([]rune, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, []rune(s)...)
	}
	return string(out)
}

// truncate shortens s to fit max columns, appending an ellipsis when cut.
func truncate(scr uv.Screen, s string, max int) string {
	m := scr.WidthMethod()
	if m.StringWidth(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	var b []rune
	w := 0
	for _, r := range s {
		rw := m.StringWidth(string(r))
		if w+rw > max-1 {
			return string(b) + "…"
		}
		b = append(b, r)
		w += rw
	}
	return string(b) + "…"
}

// wrapText splits s into lines of at most width columns, wrapping on
// explicit newlines and then greedily per rune.
func wrapText(scr uv.Screen, s string, width int) []string {
	if width < 1 {
		width = 1
	}
	m := scr.WidthMethod()
	var out []string
	for _, para := range splitLinesRaw(s) {
		if para == "" {
			out = append(out, "")
			continue
		}
		var line []rune
		w := 0
		for _, r := range para {
			rw := m.StringWidth(string(r))
			if rw <= 0 {
				rw = 1
			}
			if w+rw > width {
				out = append(out, string(line))
				line = line[:0]
				w = 0
			}
			line = append(line, r)
			w += rw
		}
		out = append(out, string(line))
	}
	return out
}

func splitLinesRaw(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
