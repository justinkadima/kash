package ui

import (
	"regexp"
	"strings"

	"github.com/justinkadima/kash/internal/ai"

	uv "github.com/charmbracelet/ultraviolet"
)

// ChipState tracks what happened to a proposed command.
type ChipState int

const (
	// ChipPending awaits the user's decision.
	ChipPending ChipState = iota
	// ChipRun was executed in the shell.
	ChipRun
	// ChipDismissed was rejected.
	ChipDismissed
)

// Chip is a shell command proposed by the model.
type Chip struct {
	Cmd   string
	State ChipState
}

// ChatMsg is one chat message. Assistant messages may contain fenced
// command blocks, which are rendered as chips instead of code text.
type ChatMsg struct {
	Role      string
	Text      string
	Streaming bool
	Chips     []*Chip
}

// hit is a clickable region recorded during drawing.
type hit struct {
	rect uv.Rectangle
	kind int // hitRun, hitEdit, hitDismiss, hitSettings, hitComplete
	chip *Chip
	idx  int // popup row (hitComplete)
}

const (
	hitRun = iota
	hitEdit
	hitDismiss
	hitSettings
	hitComplete
)

// Chat holds the visible conversation.
type Chat struct {
	msgs   []ChatMsg
	scroll int // rows hidden from the bottom (0 = follow)
}

// AddUser appends a user message.
func (c *Chat) AddUser(text string) {
	c.msgs = append(c.msgs, ChatMsg{Role: "user", Text: text})
	c.scroll = 0
}

// AddAssistant appends an empty assistant message in streaming mode.
func (c *Chat) AddAssistant() {
	c.msgs = append(c.msgs, ChatMsg{Role: "assistant", Streaming: true})
}

// AppendStream extends the last (streaming) message.
func (c *Chat) AppendStream(text string) {
	if n := len(c.msgs); n > 0 {
		c.msgs[n-1].Text += text
	}
}

// FinishStream marks the last message complete and returns its text.
func (c *Chat) FinishStream() string {
	if n := len(c.msgs); n > 0 {
		c.msgs[n-1].Streaming = false
		return c.msgs[n-1].Text
	}
	return ""
}

// AddNote appends a dim informational line (excluded from AI history).
func (c *Chat) AddNote(text string) {
	c.msgs = append(c.msgs, ChatMsg{Role: "note", Text: text})
	c.scroll = 0
}

// History returns the conversation for an AI request, skipping notes.
func (c *Chat) History(limit int) []ai.Message {
	var msgs []ai.Message
	for _, m := range c.msgs {
		if m.Role == "note" || m.Streaming {
			continue
		}
		msgs = append(msgs, ai.Message{Role: m.Role, Content: m.Text})
	}
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs
}

// Clear drops the whole conversation.
func (c *Chat) Clear() {
	c.msgs = nil
	c.scroll = 0
}

// SyncChips re-extracts command blocks from every assistant message,
// preserving the state of already-known chips.
func (c *Chat) SyncChips() {
	for i := range c.msgs {
		m := &c.msgs[i]
		if m.Role != "assistant" {
			continue
		}
		ext := ai.ExtractCommands(m.Text)
		if len(m.Chips) > len(ext) {
			m.Chips = m.Chips[:len(ext)]
		}
		for j := range ext {
			if j < len(m.Chips) {
				if m.Chips[j].Cmd == ext[j].Text {
					continue
				}
				// Mismatch from here on: replace with fresh chips.
				m.Chips = m.Chips[:j]
			}
			m.Chips = append(m.Chips, &Chip{Cmd: ext[j].Text})
		}
	}
}

// NewestPending returns the most recent chip still awaiting a decision.
func (c *Chat) NewestPending() (msgIdx, chipIdx int, chip *Chip, ok bool) {
	for i := len(c.msgs) - 1; i >= 0; i-- {
		m := &c.msgs[i]
		for j := len(m.Chips) - 1; j >= 0; j-- {
			if m.Chips[j].State == ChipPending {
				return i, j, m.Chips[j], true
			}
		}
	}
	return 0, 0, nil, false
}

// PendingChips lists all chips awaiting a decision, oldest first.
func (c *Chat) PendingChips() []*Chip {
	var out []*Chip
	for i := range c.msgs {
		for j := range c.msgs[i].Chips {
			if c.msgs[i].Chips[j].State == ChipPending {
				out = append(out, c.msgs[i].Chips[j])
			}
		}
	}
	return out
}

// ScrollBy adjusts the scroll offset (positive scrolls back).
func (c *Chat) ScrollBy(rows int) {
	c.scroll = max(c.scroll+rows, 0)
}

// ---- rendering ----

type chatElem struct {
	line  string
	style uv.Style
	chip  *chipView
}

type chipView struct {
	chip  *Chip
	msg   int
	lines []string
	rows  int
}

var chatFence = regexp.MustCompile("(?s)```([A-Za-z0-9_-]*)[^\n]*\n(.*?)```")

// buildElements flattens the conversation into renderable rows.
func (c *Chat) buildElements(scr uv.Screen, width int) []chatElem {
	var elems []chatElem
	for mi := range c.msgs {
		m := &c.msgs[mi]
		switch m.Role {
		case "user":
			elems = append(elems, chatElem{line: "❯ you", style: stAccent})
			for _, ln := range wrapText(scr, m.Text, width-2) {
				elems = append(elems, chatElem{line: ln})
			}
		case "note":
			elems = append(elems, chatElem{line: m.Text, style: stDim})
		default:
			elems = append(elems, c.buildAssistant(scr, m, mi, width)...)
		}
		elems = append(elems, chatElem{line: ""})
	}
	return elems
}

// buildAssistant splits a message into prose and chip elements.
func (c *Chat) buildAssistant(scr uv.Screen, m *ChatMsg, mi, width int) []chatElem {
	var elems []chatElem
	elems = append(elems, chatElem{line: "◍ assistant", style: stPurple})
	last := 0
	chipIdx := 0
	prose := func(text string) {
		text = trimBlank(text)
		if text == "" {
			return
		}
		for _, ln := range wrapText(scr, text, width-2) {
			elems = append(elems, chatElem{line: ln})
		}
	}
	// Reference code (python, go, json, …) is shown as text; only the
	// command fences below become chips.
	code := func(text string) {
		for _, ln := range wrapText(scr, strings.Trim(text, "\n"), width-4) {
			elems = append(elems, chatElem{line: "  " + ln, style: stDim})
		}
	}
	for _, mm := range chatFence.FindAllStringSubmatchIndex(m.Text, -1) {
		prose(m.Text[last:mm[0]])
		lang := m.Text[mm[2]:mm[3]]
		if ai.IsCommandLang(lang) && chipIdx < len(m.Chips) {
			elems = append(elems, c.newChipView(scr, m.Chips[chipIdx], mi, width))
			chipIdx++
		} else {
			// Non-command fence, or a command fence that produced no chip
			// (e.g. an empty block): render the code itself so nothing the
			// model said silently disappears.
			code(m.Text[mm[4]:mm[5]])
		}
		last = mm[1]
	}
	prose(m.Text[last:])
	if m.Streaming {
		if open := openFence(m.Text); open != "" {
			for _, ln := range wrapText(scr, open, width-2) {
				elems = append(elems, chatElem{line: ln, style: stDim})
			}
		}
		if m.Text == "" {
			elems = append(elems, chatElem{line: "…", style: stDim})
		}
	}
	return elems
}

// openFence returns the content of an unterminated fenced block, if any.
func openFence(text string) string {
	end := 0
	for _, mm := range chatFence.FindAllStringSubmatchIndex(text, -1) {
		end = mm[1]
	}
	rest := text[end:]
	i := strings.Index(rest, "```")
	if i < 0 {
		return ""
	}
	rest = rest[i+3:]
	if j := strings.Index(rest, "\n"); j >= 0 {
		rest = rest[j+1:]
	} else {
		return ""
	}
	return trimBlank(rest)
}

func trimBlank(s string) string {
	return strings.TrimSpace(strings.Trim(s, "\n "))
}

func (c *Chat) newChipView(scr uv.Screen, chip *Chip, mi, width int) chatElem {
	cv := &chipView{chip: chip, msg: mi}
	cv.lines = wrapText(scr, chip.Cmd, width-8)
	cv.rows = len(cv.lines) + 3 // top, bottom, actions
	return chatElem{chip: cv}
}

// DrawBody renders the chat into rect, recording chip hit rects.
func (c *Chat) DrawBody(scr uv.Screen, rect uv.Rectangle, hits *[]hit) {
	elems := c.buildElements(scr, rect.Dx())
	total := 0
	for _, e := range elems {
		if e.chip != nil {
			total += e.chip.rows
		} else {
			total++
		}
	}
	maxScroll := max(0, total-rect.Dy())
	if c.scroll > maxScroll {
		c.scroll = maxScroll
	}
	skip := max(0, total-rect.Dy()-c.scroll)
	y := 0
	drew := false
	for _, e := range elems {
		rows := 1
		if e.chip != nil {
			rows = e.chip.rows
		}
		if skip > 0 {
			skip -= rows
			continue // chips snap-skip; lines drop
		}
		if y >= rect.Dy() {
			break
		}
		if e.chip != nil {
			c.drawChip(scr, rect, y, e.chip, hits)
			y += rows
		} else {
			if e.line != "" {
				putStr(scr, rect, 1, y, e.line, e.style)
			}
			y++
		}
		drew = true
	}
	if !drew && rect.Dy() > 0 {
		putStr(scr, rect, 1, 0, "ask anything — model output appears here", stDim)
	}
}

// drawChip renders one command chip anchored at row y.
func (c *Chat) drawChip(scr uv.Screen, rect uv.Rectangle, y int, cv *chipView, hits *[]hit) {
	w := rect.Dx() - 4
	boxSt := stAccent
	switch cv.chip.State {
	case ChipRun:
		boxSt = stGreen
	case ChipDismissed:
		boxSt = stDim
	}
	// Top border + title.
	top := "╭" + repeat("─", max(0, w-2)) + "╮"
	putStr(scr, rect, 2, y, top, boxSt)
	putStr(scr, rect, 3, y, "─ cmd ", boxSt)
	// Body.
	for i, ln := range cv.lines {
		row := y + 1 + i
		if row >= rect.Dy() {
			return
		}
		putStr(scr, rect, 2, row, "│", boxSt)
		putStr(scr, rect, 4, row, truncate(scr, ln, w-6), uv.Style{})
		putStr(scr, rect, 2+w-1, row, "│", boxSt)
	}
	bottomRow := y + 1 + len(cv.lines)
	if bottomRow >= rect.Dy() {
		return
	}
	putStr(scr, rect, 2, bottomRow, "╰"+repeat("─", max(0, w-2))+"╯", boxSt)
	// Actions.
	actionRow := bottomRow + 1
	if actionRow >= rect.Dy() {
		return
	}
	switch cv.chip.State {
	case ChipRun:
		putStr(scr, rect, 4, actionRow, "✓ sent to terminal", stGreen)
	case ChipDismissed:
		putStr(scr, rect, 4, actionRow, "✕ dismissed", stDim)
	default:
		x := putStr(scr, rect, 4, actionRow, "▸ run", stGreen)
		*hits = append(*hits, hit{rect: uv.Rect(rect.Min.X+4, rect.Min.Y+actionRow, 5, 1), kind: hitRun, chip: cv.chip})
		x = putStr(scr, rect, x+2, actionRow, "✎ edit", stYellow)
		*hits = append(*hits, hit{rect: uv.Rect(rect.Min.X+x-5, rect.Min.Y+actionRow, 6, 1), kind: hitEdit, chip: cv.chip})
		x = putStr(scr, rect, x+2, actionRow, "✕ dismiss", stRed)
		*hits = append(*hits, hit{rect: uv.Rect(rect.Min.X+x-9, rect.Min.Y+actionRow, 10, 1), kind: hitDismiss, chip: cv.chip})
	}
}
