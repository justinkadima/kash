package ui

import (
	"strings"
	"testing"

	"github.com/justinkadima/kash/internal/config"

	uv "github.com/charmbracelet/ultraviolet"
)

// dump renders a screen to trimmed text lines for assertions.
func dump(scr uv.Screen) []string {
	r := scr.Bounds()
	lines := make([]string, r.Dy())
	for y := 0; y < r.Dy(); y++ {
		var b strings.Builder
		for x := 0; x < r.Dx(); x++ {
			if c := scr.CellAt(x, y); c != nil && c.Content != "" {
				b.WriteString(c.Content)
			} else {
				b.WriteByte(' ')
			}
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	return lines
}

func containsLine(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func TestWelcomeOverlay(t *testing.T) {
	a := &App{cfg: config.Default()}
	scr := uv.NewScreenBuffer(100, 44)
	a.drawWelcome(scr, scr.Bounds())
	lines := dump(scr)
	for _, want := range []string{"welcome", "✕", "got it", "ctrl+g", "F2 / ctrl+b", "@selection"} {
		if !containsLine(lines, want) {
			t.Errorf("welcome panel missing %q; got %q", want, lines)
		}
	}
}

func TestLayoutChatHidden(t *testing.T) {
	a := &App{cfg: config.Default()}

	a.layout(uv.Rect(0, 0, 100, 30))
	if got := a.rectTerm.Dx(); got != 100-38 {
		t.Errorf("visible: terminal width = %d, want %d", got, 100-38)
	}
	if got := a.rectChat.Dx(); got != 38 {
		t.Errorf("visible: chat width = %d, want 38", got)
	}

	a.chatHidden = true
	a.layout(uv.Rect(0, 0, 100, 30))
	if got := a.rectTerm.Dx(); got != 100 {
		t.Errorf("hidden: terminal width = %d, want 100", got)
	}
	if got := a.rectChat.Dx(); got != 0 {
		t.Errorf("hidden: chat width = %d, want 0", got)
	}
	// Zero-width rects must not intersect a click at the right edge.
	if inRect(a.rectChat, 99, 15) {
		t.Error("hidden: chat rect claims hits")
	}
}

func TestChatRenderWithChip(t *testing.T) {
	var c Chat
	c.AddUser("list files")
	c.AddAssistant()
	c.AppendStream("Here you go:\n\n```bash\nls -la\n```\n\nThat lists them.")
	c.FinishStream()
	c.SyncChips()

	scr := uv.NewScreenBuffer(44, 20)
	var hits []hit
	c.DrawBody(scr, scr.Bounds(), &hits)

	lines := dump(scr)
	for _, want := range []string{"❯ you", "list files", "◍ assistant", "Here you go:", "ls -la", "That lists them."} {
		if !containsLine(lines, want) {
			t.Errorf("expected line containing %q; got %q", want, lines)
		}
	}
	if len(hits) != 3 {
		t.Fatalf("expected 3 chip hit rects, got %d", len(hits))
	}
	for _, kind := range []int{hitRun, hitEdit, hitDismiss} {
		found := false
		for _, h := range hits {
			if h.kind == kind {
				found = true
			}
		}
		if !found {
			t.Errorf("missing hit kind %d", kind)
		}
	}

	// The chip must exist and be pending.
	_, _, chip, ok := c.NewestPending()
	if !ok || chip.Cmd != "ls -la" {
		t.Fatalf("NewestPending = %v, %v", chip, ok)
	}

	// Dismissing should be reflected in the next draw.
	chip.State = ChipDismissed
	scr = uv.NewScreenBuffer(44, 20)
	hits = nil
	c.DrawBody(scr, scr.Bounds(), &hits)
	lines = dump(scr)
	if containsLine(lines, "▸ run") || len(hits) != 0 {
		t.Errorf("dismissed chip should not offer actions; hits=%d; lines=%q", len(hits), lines)
	}
	if !containsLine(lines, "✕ dismissed") {
		t.Errorf("dismissed state should be visible; got %q", lines)
	}
}

func lineIndex(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

// Non-command code blocks (python, go, …) must render as text in the
// chat, not be swallowed by the chip path; chips must appear at their
// own fence's position, not shifted by preceding reference blocks.
func TestChatRenderNonCommandCode(t *testing.T) {
	var c Chat
	c.AddAssistant()
	c.AppendStream("Use this:\n\n```python\nprint('hi')\n```\n\nThen run it:\n\n```bash\npython3 t.py\n```\n\nDone.")
	c.FinishStream()
	c.SyncChips()
	if len(c.msgs[0].Chips) != 1 {
		t.Fatalf("expected exactly 1 chip, got %d", len(c.msgs[0].Chips))
	}

	scr := uv.NewScreenBuffer(44, 24)
	var hits []hit
	c.DrawBody(scr, scr.Bounds(), &hits)
	lines := dump(scr)
	for _, want := range []string{"Use this:", "print('hi')", "Then run it:", "python3 t.py", "Done."} {
		if !containsLine(lines, want) {
			t.Errorf("missing %q in render; got %q", want, lines)
		}
	}
	// The python code must render before the chip box (the bash block),
	// i.e. the chip must not be displaced onto the python block.
	py := lineIndex(lines, "print('hi')")
	chip := lineIndex(lines, "─ cmd")
	if py < 0 || chip < 0 || py > chip {
		t.Errorf("python line %d should precede chip %d; got %q", py, chip, lines)
	}
}

func TestChatStreamingOpenFence(t *testing.T) {
	var c Chat
	c.AddAssistant()
	// stream an unterminated command block
	c.AppendStream("Running:\n\n```bash\ngit sta")
	c.SyncChips()

	scr := uv.NewScreenBuffer(44, 20)
	var hits []hit
	c.DrawBody(scr, scr.Bounds(), &hits)

	lines := dump(scr)
	if !containsLine(lines, "git sta") {
		t.Errorf("open fence content should be visible; got %q", lines)
	}
	if len(hits) != 0 {
		t.Errorf("no chip hits expected for unterminated block, got %d", len(hits))
	}
	if _, _, _, ok := c.NewestPending(); ok {
		t.Errorf("no pending chip expected for unterminated block")
	}
}

func TestChatHistorySkipsNotes(t *testing.T) {
	var c Chat
	c.AddUser("one")
	c.AddAssistant()
	c.AppendStream("answer")
	c.FinishStream()
	c.AddNote("⚠ something broke")
	c.AddUser("two")
	msgs := c.History(0)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 history messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.Role == "note" {
			t.Errorf("note leaked into history")
		}
	}
}

func TestSettingsModalApply(t *testing.T) {
	m := newSettingsModal(newTestConfig(t))
	// set field values through the struct directly for the test
	m.fields[0].val = "http://localhost:8080"
	m.fields[1].val = "llama3"
	m.fields[3].val = "0.5"
	m.fields[4].val = "100"
	m.fields[5].val = "45"
	m.fields[6].val = "on"

	cfg := newTestConfig(t)
	if err := m.apply(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://localhost:8080" || cfg.Model != "llama3" {
		t.Errorf("apply mismatch: %+v", cfg)
	}
	if cfg.Temperature != 0.5 || cfg.ContextLines != 100 || cfg.ChatRatio != 0.45 || !cfg.AutoRun {
		t.Errorf("apply mismatch: %+v", cfg)
	}

	m.fields[3].val = "abc"
	if err := m.apply(cfg); err == nil {
		t.Error("expected error for bad temperature")
	}

	m.fields[1].val = ""
	if err := m.apply(cfg); err == nil {
		t.Error("expected error for empty model")
	}
}

func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return config.Default()
}
