package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// helpLines lists the key bindings. Ctrl bindings work everywhere (raw
// control codes, no terminal configuration); Alt bindings need
// Option-as-Esc mode on macOS.
var helpLines = []struct {
	keys string
	desc string
}{
	{"ctrl+g", "terminal pane → focus chat · chat → help"},
	{"alt+space / click", "switch focus: terminal ⇄ chat"},
	{"ctrl+t / alt+t", "toggle chat ⇄ term input mode"},
	{"enter", "chat: send message · term input: run line"},
	{"ctrl+s / alt+s", "settings (Ollama URL, model, …)"},
	{"ctrl+r / alt+r", "run the newest proposed command"},
	{"alt+e", "edit the newest command (loads it into term input)"},
	{"ctrl+d / alt+d", "dismiss the newest proposed command"},
	{"ctrl+l / alt+c", "clear the conversation"},
	{"ctrl+q / alt+q", "quit (from chat input)"},
	{"drag mouse", "select text in the terminal"},
	{"right-click / alt+a", "attach selection as chat context"},
	{"", ""},
	{"@ in chat input", "reference things in the message:"},
	{"", "@selection — the current terminal selection"},
	{"", "@path/to/file — file content (capped)"},
	{"", "@dir/ — a directory listing"},
	{"", "popup: ↑↓ navigate · tab complete · esc close"},
	{"", ""},
	{"ctrl+c", "chat: cancel stream · terminal: SIGINT to shell"},
	{"esc", "cancel stream · leave term input"},
	{"wheel / pgup pgdn", "scroll terminal / chat history"},
	{"", ""},
	{"macOS:", "if alt+… does nothing, your terminal composes"},
	{"", "special characters with Option. Enable meta mode:"},
	{"", "Terminal: Settings→Profiles→Keyboard→Use Option as Meta"},
	{"", "iTerm2: Profiles→Keys→Option key sends: Esc+"},
	{"", "(Ghostty, kitty, WezTerm, tmux send alt natively)"},
}

// drawHelp renders the help overlay centered over area. It doubles as a
// key tester: the last key press is shown, so users can check what their
// terminal actually delivers.
func (a *App) drawHelp(scr uv.Screen, area uv.Rectangle) {
	w := min(64, area.Dx()-2)
	h := min(len(helpLines)+4, area.Dy()-2)
	x0 := area.Min.X + (area.Dx()-w)/2
	y0 := area.Min.Y + (area.Dy()-h)/2
	rect := uv.Rect(x0, y0, w, h)

	fillRect(scr, rect, uv.Style{})
	inner := drawBox(scr, rect, stAccent, "help")
	for i, l := range helpLines {
		if i >= inner.Dy()-2 {
			break
		}
		putStr(scr, inner, 1, i, l.keys, stYellow)
		putStr(scr, inner, 21, i, truncate(scr, l.desc, inner.Dx()-22), uv.Style{})
	}
	// Key tester line: shows what the terminal last delivered to con.
	last := a.lastKey
	if last == "" {
		last = "…press any key"
	}
	putStr(scr, inner, 1, inner.Dy()-2, "last key: "+last, stDim)
	putStr(scr, inner, 1, inner.Dy()-1, "esc / ctrl+g to close", stDim)
}
