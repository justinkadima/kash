package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// InputBar is a single-line text editor with history, drawn as a bordered
// bar. It knows nothing about what the text is for; the App decides.
type InputBar struct {
	runes   []rune
	pos     int
	hist    []string
	histIdx int
}

// Text returns the current content.
func (in *InputBar) Text() string { return string(in.runes) }

// SetText replaces the content and puts the cursor at the end.
func (in *InputBar) SetText(s string) {
	in.runes = []rune(strings.TrimRight(s, "\r\n"))
	in.pos = len(in.runes)
}

// Clear empties the bar and resets history navigation.
func (in *InputBar) Clear() {
	in.runes = in.runes[:0]
	in.pos = 0
	in.histIdx = len(in.hist)
}

// Insert inserts s at the cursor.
func (in *InputBar) Insert(s string) {
	r := []rune(s)
	tail := append([]rune{}, in.runes[in.pos:]...)
	in.runes = append(append(in.runes[:in.pos], r...), tail...)
	in.pos += len(r)
}

// PushHistory records a submitted entry.
func (in *InputBar) PushHistory(s string) {
	if s == "" {
		return
	}
	if n := len(in.hist); n > 0 && in.hist[n-1] == s {
		in.histIdx = len(in.hist)
		return
	}
	in.hist = append(in.hist, s)
	if len(in.hist) > 100 {
		in.hist = in.hist[len(in.hist)-100:]
	}
	in.histIdx = len(in.hist)
}

// HandleKey applies an editing key. Returns true if the key was consumed.
func (in *InputBar) HandleKey(k uv.Key) bool {
	ctrl := k.Mod&uv.ModCtrl != 0
	switch k.Code {
	case uv.KeyBackspace:
		if in.pos > 0 {
			in.runes = append(in.runes[:in.pos-1], in.runes[in.pos:]...)
			in.pos--
		}
		return true
	case uv.KeyDelete:
		if in.pos < len(in.runes) {
			in.runes = append(in.runes[:in.pos], in.runes[in.pos+1:]...)
		}
		return true
	case uv.KeyLeft:
		if !ctrl && in.pos > 0 {
			in.pos--
		}
		return true
	case uv.KeyRight:
		if !ctrl && in.pos < len(in.runes) {
			in.pos++
		}
		return true
	case uv.KeyHome, uv.KeyUp:
		if ctrl || k.Code == uv.KeyHome {
			in.pos = 0
			return true
		}
		return in.history(-1)
	case uv.KeyEnd, uv.KeyDown:
		if ctrl || k.Code == uv.KeyEnd {
			in.pos = len(in.runes)
			return true
		}
		return in.history(1)
	}
	if ctrl {
		switch k.Code {
		case 'a':
			in.pos = 0
			return true
		case 'e':
			in.pos = len(in.runes)
			return true
		case 'u':
			in.runes = append([]rune{}, in.runes[in.pos:]...)
			in.pos = 0
			return true
		case 'k':
			in.runes = in.runes[:in.pos]
			return true
		case 'w':
			i := in.pos
			for i > 0 && in.runes[i-1] == ' ' {
				i--
			}
			for i > 0 && in.runes[i-1] != ' ' {
				i--
			}
			in.runes = append(in.runes[:i], in.runes[in.pos:]...)
			in.pos = i
			return true
		}
		return false
	}
	// Printable input: no ctrl/alt/meta modifiers.
	if k.Mod&(uv.ModCtrl|uv.ModAlt|uv.ModMeta) != 0 {
		return false
	}
	if k.Text == "" {
		return false
	}
	in.Insert(k.Text)
	return true
}

func (in *InputBar) history(dir int) bool {
	if len(in.hist) == 0 {
		return true
	}
	idx := in.histIdx
	switch {
	case dir < 0 && idx > 0:
		idx--
	case dir > 0 && idx < len(in.hist):
		idx++
	}
	if idx != in.histIdx {
		in.histIdx = idx
		if idx == len(in.hist) {
			in.SetText("")
		} else {
			in.SetText(in.hist[idx])
		}
	}
	return true
}

// Draw renders the bar into rect (which must be 3 rows tall) and returns
// the absolute position for the terminal cursor. The prefix identifies the
// mode (e.g. "chat ▸" or "term ❯").
func (in *InputBar) Draw(scr uv.Screen, rect uv.Rectangle, prefix string, prefixStyle uv.Style, focused bool) uv.Position {
	border := stBorder
	if focused {
		border = stAccent
	}
	inner := drawBox(scr, rect, border, "")
	m := scr.WidthMethod()

	x := putStr(scr, inner, 1, 0, prefix, prefixStyle)

	// Horizontal scroll window so the cursor stays visible.
	prefixW := m.StringWidth(prefix) + 1
	viewW := inner.Dx() - 1 - prefixW
	if viewW < 1 {
		viewW = 1
	}
	before := in.widthTo(m, in.pos)
	after := in.widthTo(m, len(in.runes))
	scroll := 0
	if before > viewW-1 {
		scroll = before - (viewW - 1)
	}
	if after-scroll < viewW {
		// keep as much trailing text visible as fits
		scroll = max(0, after-viewW)
	}

	cursorCol := prefixW + in.widthTo(m, in.pos) - scroll
	for i, r := range in.runes {
		w := in.widthTo(m, i+1) - in.widthTo(m, i)
		if w <= 0 {
			continue
		}
		col := in.widthTo(m, i) - scroll
		if col < 0 || col+w > viewW {
			continue
		}
		c := uv.NewCell(m, string(r))
		c.Style = uv.Style{}
		scr.SetCell(inner.Min.X+prefixW+col, inner.Min.Y, c)
	}
	_ = x
	return uv.Position{X: inner.Min.X + cursorCol, Y: inner.Min.Y}
}

func (in *InputBar) widthTo(m uv.WidthMethod, idx int) int {
	w := 0
	for i := 0; i < idx && i < len(in.runes); i++ {
		w += max(m.StringWidth(string(in.runes[i])), 1)
	}
	return w
}
