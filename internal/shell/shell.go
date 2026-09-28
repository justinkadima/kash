// Package shell runs the user's shell in a pty and pipes its I/O through a
// vt terminal emulator, exposing the parsed screen state to the UI.
package shell

import (
	"os"
	"os/exec"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	vt "github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Shell couples a pty child process with a vt emulator. The pty output pump
// feeds bytes into the emulator (SafeEmulator serializes access); the input
// pump drains emulator-encoded keystrokes back into the pty.
type Shell struct {
	vt  *vt.SafeEmulator
	tty *os.File
	cmd *exec.Cmd

	wake   func() // asks the UI loop to redraw
	onExit func() // tells the UI loop the child exited

	// ioMu serializes emulator access between the output pump (Write)
	// and the render thread (batched cell reads).
	ioMu sync.Mutex

	mu         sync.Mutex
	title      string
	cwd        string
	childMouse bool // child application enabled mouse reporting
	altScreen  bool // child switched to the alternate screen
	cursorVis  bool
	exited     bool
}

// Detect picks the shell binary: explicit override, then $SHELL, then
// platform fallbacks.
func Detect(override string) string {
	if override != "" {
		return override
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	for _, cand := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return "/bin/sh"
}

// New spawns the shell at the given size. wake is called after every chunk
// of parsed output (it should be cheap and non-reentrant); onExit is called
// exactly once when the child process terminates.
func New(cols, rows int, shellPath string, wake, onExit func()) (*Shell, error) {
	s := &Shell{wake: wake, onExit: onExit, cursorVis: true}
	s.vt = vt.NewSafeEmulator(cols, rows)
	s.vt.SetCallbacks(vt.Callbacks{
		Title: func(t string) {
			s.mu.Lock()
			s.title = t
			s.mu.Unlock()
		},
		WorkingDirectory: func(d string) {
			s.mu.Lock()
			s.cwd = d
			s.mu.Unlock()
		},
		AltScreen: func(b bool) {
			s.mu.Lock()
			s.altScreen = b
			s.mu.Unlock()
		},
		CursorVisibility: func(b bool) {
			s.mu.Lock()
			s.cursorVis = b
			s.mu.Unlock()
		},
		EnableMode: func(m ansi.Mode) {
			if isMouseMode(m) {
				s.mu.Lock()
				s.childMouse = true
				s.mu.Unlock()
			}
		},
		DisableMode: func(m ansi.Mode) {
			if isMouseMode(m) {
				s.mu.Lock()
				s.childMouse = false
				s.mu.Unlock()
			}
		},
	})

	s.cmd = exec.Command(shellPath)
	s.cmd.Env = os.Environ()
	tty, err := pty.StartWithSize(s.cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	s.tty = tty

	go s.pumpOutput()
	go s.pumpInput()
	go s.waitExit()

	return s, nil
}

// Lock / Unlock bracket batched emulator reads (rendering, text
// extraction) against the output pump's writes.
func (s *Shell) Lock()   { s.ioMu.Lock() }
func (s *Shell) Unlock() { s.ioMu.Unlock() }

func isMouseMode(m ansi.Mode) bool {
	switch m {
	case ansi.ModeMouseX10,
		ansi.ModeMouseNormal,
		ansi.ModeMouseHighlight,
		ansi.ModeMouseButtonEvent,
		ansi.ModeMouseAnyEvent:
		return true
	}
	return false
}

// pumpOutput reads raw child output and parses it into the emulator.
// When the child exits the pty read fails with EIO/EOF and we stop.
func (s *Shell) pumpOutput() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.tty.Read(buf)
		if n > 0 {
			s.ioMu.Lock()
			_, _ = s.vt.Write(buf[:n])
			s.ioMu.Unlock()
			s.wake()
		}
		if err != nil {
			return
		}
	}
}

// pumpInput forwards emulator-encoded input bytes to the pty.
func (s *Shell) pumpInput() {
	buf := make([]byte, 4*1024)
	for {
		n, err := s.vt.Read(buf)
		if n > 0 {
			if _, werr := s.tty.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Shell) waitExit() {
	_ = s.cmd.Wait()
	s.mu.Lock()
	s.exited = true
	s.mu.Unlock()
	s.onExit()
}

// ---- input ----

// SendKey forwards a key event to the child (encoded per its current modes).
//
// vt.SendKey only implements legacy key encodings. Hosts using the kitty
// keyboard protocol report 'A' as {Code:'a', Mod:Shift, Text:"A"}, which vt
// drops (its default case refuses any event with modifiers set). Normalize
// printable events to raw text — exactly what a terminal delivers for
// printable input — and pass everything else through untouched.
func (s *Shell) SendKey(k uv.KeyEvent) {
	if kp, ok := k.(uv.KeyPressEvent); ok {
		key := kp.Key()
		if key.Text != "" && key.Mod&^uv.ModShift == 0 {
			s.vt.SendText(key.Text)
			return
		}
	}
	s.vt.SendKey(k)
}

// SendMouse forwards a mouse event, translated to child-relative coordinates
// by the caller; vt drops it unless the child enabled mouse reporting.
func (s *Shell) SendMouse(m uv.MouseEvent) { s.vt.SendMouse(m) }

// SendText writes raw text to the child input.
func (s *Shell) SendText(t string) { s.vt.SendText(t) }

// Paste pastes text into the child, honoring its bracketed-paste mode.
func (s *Shell) Paste(t string) { s.vt.Paste(t) }

// Run sends text followed by a carriage return. Multi-line text is sent
// line by line so the shell treats each as its own command.
func (s *Shell) Run(text string) {
	for _, line := range splitLines(text) {
		s.vt.SendText(line)
		s.vt.SendText("\r")
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// ---- lifecycle ----

// Resize propagates a new pane size to both the emulator and the pty.
func (s *Shell) Resize(cols, rows int) {
	s.ioMu.Lock()
	s.vt.Resize(cols, rows)
	s.ioMu.Unlock()
	_ = pty.Setsize(s.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Close tears down the child and the emulator.
func (s *Shell) Close() {
	_ = s.vt.Close()
	_ = s.tty.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// ---- read-only state for the UI ----

// Snapshot bundles the child-state the status bar and render loop need,
// under one lock acquisition.
type Snapshot struct {
	Title      string
	Cwd        string
	ChildMouse bool
	AltScreen  bool
	CursorVis  bool
	Exited     bool
}

func (s *Shell) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{
		Title:      s.title,
		Cwd:        s.cwd,
		ChildMouse: s.childMouse,
		AltScreen:  s.altScreen,
		CursorVis:  s.cursorVis,
		Exited:     s.exited,
	}
}

// Terminal returns the underlying emulator for cell-level rendering.
func (s *Shell) Terminal() *vt.SafeEmulator { return s.vt }
