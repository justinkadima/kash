package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

var helpLines = []struct {
	keys string
	desc string
}{
	{"alt+space", "switch focus: terminal ⇄ chat"},
	{"click pane", "focus the clicked pane"},
	{"alt+s", "settings (Ollama URL, model, …)"},
	{"alt+h", "toggle this help"},
	{"alt+q", "quit"},
	{"alt+c", "clear the conversation"},
	{"drag mouse", "select text in the terminal"},
	{"right-click", "attach selection as chat context"},
	{"alt+a", "attach selection as context"},
	{"alt+r", "run the newest proposed command"},
	{"alt+e", "edit the newest proposed command"},
	{"alt+t", "toggle chat ⇄ term input mode"},
	{"enter", "chat: send message · term-mode: run line"},
	{"ctrl+c", "chat: cancel stream · terminal: SIGINT to shell"},
	{"esc", "cancel stream · leave term-mode"},
	{"wheel / pgup", "scroll terminal / chat history"},
	{"", ""},
	{"proposed commands show as chips:", ""},
	{"  run — execute in the shell (unsandboxed!)", ""},
	{"  edit — tweak, then enter runs it", ""},
	{"  dismiss — reject", ""},
}

// drawHelp renders the help overlay centered over area.
func (a *App) drawHelp(scr uv.Screen, area uv.Rectangle) {
	w := min(64, area.Dx()-2)
	h := min(len(helpLines)+3, area.Dy()-2)
	x0 := area.Min.X + (area.Dx()-w)/2
	y0 := area.Min.Y + (area.Dy()-h)/2
	rect := uv.Rect(x0, y0, w, h)

	fillRect(scr, rect, uv.Style{})
	inner := drawBox(scr, rect, stAccent, "help")
	for i, l := range helpLines {
		if i >= inner.Dy()-1 {
			break
		}
		putStr(scr, inner, 1, i, l.keys, stYellow)
		putStr(scr, inner, 16, i, truncate(scr, l.desc, inner.Dx()-17), uv.Style{})
	}
	putStr(scr, inner, 1, inner.Dy()-1, "press any key to close", stDim)
}
