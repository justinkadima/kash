package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Chat @-references. In the chat input, a standalone token starting with
// '@' can pull external content into the message sent to the model:
//
//	@selection       the current terminal selection (what you highlighted)
//	@path/to/file    a file's content (capped)
//	@path/to/dir/    a directory listing
//
// A token only counts as a reference when it is preceded by whitespace
// (so email addresses like foo@bar.com are left alone) and trailing
// punctuation is ignored ("@go.mod," references go.mod).

const (
	refMaxBytes = 8000 // per file
	refMaxLines = 400  // per file
	refMaxDir   = 50   // entries per listing
	refMaxComp  = 8    // filesystem items in the popup
)

// mention is one resolved @reference.
type mention struct {
	Token string // as typed, e.g. "@src/main.go"
	Kind  string // "selection", "file", "dir"
	Text  string // resolved content ("" on error)
	Err   string // resolution failure, "" on success
}

// compItem is one row in the @-autocomplete popup.
type compItem struct {
	label  string // displayed name
	detail string // displayed hint
	insert string // text substituted for the token (starts with @)
}

// mentionTokens extracts deduplicated, order-preserving reference tokens
// from a chat message.
func mentionTokens(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range strings.Fields(text) {
		if !strings.HasPrefix(f, "@") || f == "@" {
			continue
		}
		tok := "@" + strings.TrimRight(f[1:], ".,;:!?)]\"'")
		if tok == "@" || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// baseDir returns the directory relative @paths resolve against: the
// shell's cwd when the shell reports it (OSC 7), else con's cwd.
func (a *App) baseDir() string {
	if a.shell != nil {
		if cwd := a.shell.Snapshot().Cwd; cwd != "" {
			if st, err := os.Stat(cwd); err == nil && st.IsDir() {
				return cwd
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// resolveMentions turns the tokens in text into mention records.
func (a *App) resolveMentions(text string) []mention {
	var out []mention
	for _, tok := range mentionTokens(text) {
		if strings.EqualFold(tok, "@selection") {
			m := mention{Token: tok, Kind: "selection"}
			if txt := a.selectionText(); txt != "" {
				m.Text = txt
			} else {
				m.Err = "nothing is selected in the terminal"
			}
			out = append(out, m)
			continue
		}
		out = append(out, resolvePathMention(tok, a.baseDir()))
	}
	return out
}

// resolvePathMention reads a file (capped) or lists a directory.
func resolvePathMention(tok, base string) mention {
	m := mention{Token: tok, Kind: "file"}
	p := expandHome(strings.TrimPrefix(tok, "@"))
	if p == "" {
		m.Err = "empty path"
		return m
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	info, err := os.Stat(p)
	if err != nil {
		m.Err = "not found"
		return m
	}
	if info.IsDir() {
		m.Kind = "dir"
		m.Text = dirListing(p)
		return m
	}
	data, note, err := readFileCapped(p)
	if err != nil {
		m.Err = err.Error()
		return m
	}
	m.Text = data
	if note != "" {
		m.Text += "\n[" + note + "]"
	}
	return m
}

// expandHome resolves a leading ~ to the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// readFileCapped reads up to refMaxBytes / refMaxLines of a file and
// reports what it truncated. Binary files are refused.
func readFileCapped(path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	size := info.Size()
	buf := make([]byte, min(size, int64(refMaxBytes)))
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", "", err
	}
	data := buf[:n]
	if idx := indexByte(data, 0); idx >= 0 {
		return "", "", fmt.Errorf("binary file (%d bytes) not included", size)
	}
	note := ""
	if int64(n) < size {
		note = fmt.Sprintf("truncated: first %d of %d bytes", n, size)
	}
	// line cap
	lines := 0
	cut := -1
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines >= refMaxLines {
				cut = i
				break
			}
		}
	}
	if cut >= 0 {
		data = data[:cut]
		note = fmt.Sprintf("truncated: first %d lines", refMaxLines)
	}
	return strings.TrimRight(string(data), "\n"), note, nil
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// dirListing renders a directory listing (entries + type marker).
func dirListing(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "(could not read directory: " + err.Error() + ")"
	}
	var b strings.Builder
	n := 0
	for _, e := range entries {
		if n >= refMaxDir {
			b.WriteString("…\n")
			break
		}
		if e.IsDir() {
			fmt.Fprintf(&b, "%s/\n", e.Name())
		} else if info, err := e.Info(); err == nil {
			fmt.Fprintf(&b, "%s (%d bytes)\n", e.Name(), info.Size())
		} else {
			fmt.Fprintf(&b, "%s\n", e.Name())
		}
		n++
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatRefBlock renders resolved mentions as the block appended beneath
// the user message for this turn only.
func formatRefBlock(ms []mention) string {
	var b strings.Builder
	for _, m := range ms {
		b.WriteString("\n--- " + m.Token)
		if m.Err != "" {
			b.WriteString(": " + m.Err + " ---\n")
			continue
		}
		b.WriteString(" (" + m.Kind + ", included this turn only) ---\n")
		b.WriteString(m.Text)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// failedMentionNotes lists human-visible warnings for broken mentions.
func failedMentionNotes(ms []mention) []string {
	var out []string
	for _, m := range ms {
		if m.Err != "" {
			out = append(out, "⚠ "+m.Token+": "+m.Err)
		}
	}
	return out
}

// ---- @-autocomplete ----

// buildCompletions lists popup candidates for the token being typed.
// base is the directory relative paths resolve against.
func buildCompletions(token, base string, selHave bool) []compItem {
	prefix := strings.TrimPrefix(token, "@")
	var items []compItem

	// selection pseudo-entry
	if prefix == "" || strings.HasPrefix("selection", prefix) {
		detail := "current terminal selection"
		if !selHave {
			detail = "selection (nothing selected yet)"
		}
		items = append(items, compItem{label: "selection", detail: detail, insert: "@selection"})
	}

	// filesystem entries: split "dir/prefix" at the last separator
	dir, filePrefix := "", prefix
	if i := strings.LastIndex(prefix, "/"); i >= 0 {
		dir, filePrefix = prefix[:i+1], prefix[i+1:]
	}
	dirPath := expandHome(dir)
	if !filepath.IsAbs(dirPath) {
		dirPath = filepath.Join(base, dirPath)
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return items
	}
	n := 0
	for _, e := range entries {
		if n >= refMaxComp {
			break
		}
		name := e.Name()
		// hidden files only when the prefix asks for them
		if strings.HasPrefix(name, ".") != strings.HasPrefix(filePrefix, ".") {
			continue
		}
		if !strings.HasPrefix(name, filePrefix) {
			continue
		}
		ins := "@" + dir + name
		lbl := name
		detail := "file"
		if e.IsDir() {
			ins += "/"
			lbl += "/"
			detail = "dir"
		} else if info, err := e.Info(); err == nil {
			detail = fmt.Sprintf("%d B", info.Size())
		}
		items = append(items, compItem{label: lbl, detail: detail, insert: ins})
		n++
	}
	return items
}

// isMentionSpace reports whether r separates @tokens in the input.
func isMentionSpace(r rune) bool { return r == ' ' || r == '\t' }

// updateCompletions recomputes the popup from the input state. Called
// after every edit in chat mode.
func (a *App) updateCompletions() {
	if a.mode != modeChat || a.focus != focusChat || a.modal != nil || a.help {
		a.comp.visible = false
		return
	}
	tok, ok := a.input.mentionToken()
	if !ok {
		a.comp.visible = false
		return
	}
	items := buildCompletions(tok, a.baseDir(), a.sel.have)
	if len(items) == 0 {
		a.comp.visible = false
		return
	}
	a.comp.items = items
	if a.comp.sel >= len(items) {
		a.comp.sel = 0
	}
	a.comp.visible = true
}

// applyCompletion substitutes the selected popup item into the input.
func (a *App) applyCompletion() {
	if !a.comp.visible || a.comp.sel >= len(a.comp.items) {
		return
	}
	item := a.comp.items[a.comp.sel]
	in := &a.input
	start := in.pos
	for start > 0 && !isMentionSpace(in.runes[start-1]) {
		start--
	}
	if start >= len(in.runes) || in.runes[start] != '@' {
		a.comp.visible = false
		return
	}
	ins := []rune(item.insert)
	tail := append([]rune{}, in.runes[in.pos:]...)
	in.runes = append(append(in.runes[:start], ins...), tail...)
	in.pos = start + len(ins)
	a.comp.visible = false
}

// mentionToken returns the @token the cursor is in, if any.
func (in *InputBar) mentionToken() (string, bool) {
	if in.pos <= 0 || in.pos > len(in.runes) {
		return "", false
	}
	start := in.pos
	for start > 0 && !isMentionSpace(in.runes[start-1]) {
		start--
	}
	if start >= len(in.runes) || in.runes[start] != '@' {
		return "", false
	}
	return string(in.runes[start:in.pos]), true
}

// drawCompletions renders the @-popup anchored above the chat input bar.
func (a *App) drawCompletions(scr uv.Screen, hits *[]hit) {
	n := min(len(a.comp.items), 6)
	if n == 0 {
		return
	}
	h := n + 2
	w := a.rectChat.Dx() - 4
	rect := uv.Rect(a.rectChatInput.Min.X+2, a.rectChatInput.Min.Y-h, w, h)
	fillRect(scr, rect, uv.Style{})
	inner := drawBox(scr, rect, stAccent, "@reference")
	for i := 0; i < n; i++ {
		item := a.comp.items[i]
		st := uv.Style{}
		if i == a.comp.sel {
			fillRect(scr, uv.Rect(inner.Min.X, inner.Min.Y+i, inner.Dx(), 1), stReverse)
			st = stReverse
		}
		x := putStr(scr, inner, 1, i, item.label, st)
		putStr(scr, inner, max(x+2, inner.Dx()/2), i, truncate(scr, item.detail, inner.Dx()/2-2), stDim)
		*hits = append(*hits, hit{
			rect: uv.Rect(inner.Min.X, inner.Min.Y+i, inner.Dx(), 1),
			kind: hitComplete, idx: i,
		})
	}
}
