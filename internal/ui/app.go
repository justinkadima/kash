package ui

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/justin/conterm/internal/ai"
	"github.com/justin/conterm/internal/config"
	"github.com/justin/conterm/internal/shell"

	uv "github.com/charmbracelet/ultraviolet"
)

// ---- custom loop events (injected via term.SendEvent) ----

type (
	wakeEvent         struct{}
	shellExitEvent    struct{}
	streamDeltaEvent  struct{ text string }
	streamEndEvent    struct{ err error }
	modelsResultEvent struct {
		models []string
		err    error
	}
)

// ---- focus / modes ----

type focusT int

const (
	focusTerminal focusT = iota
	focusChat
)

type inputModeT int

const (
	modeChat    inputModeT = iota // typed text goes to the AI
	modeCommand                   // typed text runs in the shell on enter
)

// App is the whole user interface: terminal pane, chat pane, status bar,
// settings and help overlays. It implements uv.Drawable.
type App struct {
	cfg       *config.Config
	cfgPath   string
	shellPath string
	client    *ai.Client

	term *uv.Terminal
	scr  *uv.TerminalScreen

	shell    *shell.Shell
	startErr error

	focus focusT
	mode  inputModeT

	input    InputBar // chat input
	cmdInput InputBar // term-mode input (edit/run commands)

	chat  Chat
	modal *SettingsModal
	help  bool

	streaming bool
	canceled  bool
	cancel    context.CancelFunc

	selCtx     string // selection text queued as context for the next message
	sel        selState
	termScroll int

	status  string
	lastKey string
	spin    int
	quit    bool

	hits []hit

	// layout rects
	rectTerm      uv.Rectangle
	rectChat      uv.Rectangle
	rectChatBody  uv.Rectangle
	rectChatInput uv.Rectangle
	rectStatus    uv.Rectangle
}

// Run drives the application until the user quits or the shell exits.
func Run(cfg *config.Config, cfgPath, shellPath string) error {
	term := uv.NewTerminal(uv.DefaultConsole(), nil)
	a := &App{
		cfg:       cfg,
		cfgPath:   cfgPath,
		shellPath: shellPath,
		client:    ai.New(cfg.BaseURL, cfg.APIKey),
		term:      term,
		focus:     focusTerminal,
	}
	if err := term.Start(); err != nil {
		return err
	}
	scr := term.Screen()
	a.scr = scr
	scr.EnterAltScreen()
	scr.HideCursor()
	scr.SetMouseMode(uv.MouseModeDrag)
	scr.SetMouseEncoding(uv.MouseEncodingSGR)
	scr.EnableBracketedPaste()
	_ = scr.Flush()

	defer term.Stop()
	defer a.closeShell()

	go a.probeModels()

	for ev := range term.Events() {
		a.handle(ev)
		if a.quit {
			break
		}
		_ = scr.Display(a)
	}
	return nil
}

func (a *App) closeShell() {
	if a.shell != nil {
		a.shell.Close()
	}
}

func (a *App) handle(ev uv.Event) {
	switch e := ev.(type) {
	case uv.KeyPressEvent:
		a.onKey(e)
	case uv.MouseClickEvent:
		a.onClick(e)
	case uv.MouseReleaseEvent:
		a.onRelease(e)
	case uv.MouseMotionEvent:
		a.onMotion(e)
	case uv.MouseWheelEvent:
		a.onWheel(e)
	case uv.WindowSizeEvent:
		a.resize(e.Width, e.Height)
	case uv.PasteEvent:
		a.onPaste(e)
	case wakeEvent:
		// redraw only
	case shellExitEvent:
		a.quit = true
	case streamDeltaEvent:
		a.onStreamDelta(e.text)
	case streamEndEvent:
		a.onStreamEnd(e.err)
	case modelsResultEvent:
		a.onModels(e.models, e.err)
	}
}

func (a *App) resize(w, h int) {
	a.scr.Resize(w, h)
	a.layout(a.scr.Bounds())
	if a.shell == nil && a.startErr == nil {
		sh, err := shell.New(
			a.rectTerm.Dx(), a.rectTerm.Dy(), a.shellPath,
			func() { a.term.SendEvent(wakeEvent{}) },
			func() { a.term.SendEvent(shellExitEvent{}) },
		)
		if err != nil {
			a.startErr = err
			return
		}
		a.shell = sh
	} else if a.shell != nil {
		a.shell.Resize(a.rectTerm.Dx(), a.rectTerm.Dy())
	}
}

func (a *App) probeModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := a.client.Models(ctx)
	a.term.SendEvent(modelsResultEvent{models: models, err: err})
}

// ---- key routing ----

func (a *App) onKey(e uv.KeyPressEvent) {
	k := e.Key()
	a.lastKey = e.Keystroke()

	if a.modal != nil {
		a.modalAction(a.modal.handleKey(k))
		return
	}
	if a.help {
		// Help doubles as a key tester: any key updates the diagnostic
		// line at the bottom; only these close it.
		if k.Code == uv.KeyEscape || k.Code == uv.KeyEnter ||
			(k.Mod&uv.ModCtrl != 0 && k.Code == 'g') {
			a.help = false
		}
		return
	}
	if k.Mod&uv.ModAlt != 0 {
		switch k.Code {
		case uv.KeySpace:
			if a.focus == focusTerminal {
				a.focus = focusChat
			} else {
				a.focus = focusTerminal
			}
			return
		case 's':
			a.modal = newSettingsModal(a.cfg)
			return
		case 'h':
			a.help = !a.help
			return
		case 'q':
			a.quit = true
			return
		case 'c':
			a.chat.Clear()
			a.selCtx = ""
			a.status = "conversation cleared"
			return
		case 'r':
			a.runNewestChip()
			return
		case 't':
			a.toggleCommandMode()
			return
		case 'e':
			a.editNewestChip()
			return
		case 'd':
			a.dismissNewestChip()
			return
		case 'a':
			a.attachSelection()
			return
		}
	}

	// Ctrl+G is the universal escape hatch: it arrives as a raw control
	// code (BEL), so every terminal delivers it without any meta/option
	// configuration. From the terminal pane it jumps to chat; from chat
	// it opens help.
	if k.Mod&uv.ModCtrl != 0 && k.Code == 'g' {
		if a.focus == focusTerminal {
			a.focus = focusChat
		} else {
			a.help = true
		}
		return
	}

	if a.focus == focusTerminal {
		if a.shell != nil {
			a.shell.SendKey(e)
		}
		return
	}

	// Chat focus. Ctrl bindings below are safe because this input is ours:
	// they are raw control codes and never reach the shell.
	if k.Mod&uv.ModCtrl != 0 {
		switch k.Code {
		case 'c':
			if a.streaming {
				a.cancelStream()
			} else {
				a.activeInput().Clear()
			}
			return
		case 's':
			a.modal = newSettingsModal(a.cfg)
			return
		case 't':
			a.toggleCommandMode()
			return
		case 'r':
			a.runNewestChip()
			return
		case 'd':
			a.dismissNewestChip()
			return
		case 'l':
			a.chat.Clear()
			a.selCtx = ""
			a.status = "conversation cleared"
			return
		case 'q':
			a.quit = true
			return
		}
		return
	}
	switch k.Code {
	case uv.KeyEscape:
		if a.streaming {
			a.cancelStream()
			return
		}
		if a.mode == modeCommand {
			a.mode = modeChat
			a.status = ""
			return
		}
		return
	case uv.KeyEnter:
		if a.mode == modeChat {
			a.sendChat()
		} else {
			a.sendCommand()
		}
		return
	case uv.KeyTab:
		if a.mode == modeChat {
			a.input.Insert("    ")
		}
		return
	case uv.KeyPgUp:
		a.chat.ScrollBy(max(3, a.rectChatBody.Dy()-2))
		return
	case uv.KeyPgDown:
		a.chat.ScrollBy(-max(3, a.rectChatBody.Dy()-2))
		return
	}
	a.activeInput().HandleKey(k)
}

func (a *App) activeInput() *InputBar {
	if a.mode == modeCommand {
		return &a.cmdInput
	}
	return &a.input
}

// toggleCommandMode switches the chat input between chatting (text goes to
// the AI) and term mode (enter runs the line in the shell).
func (a *App) toggleCommandMode() {
	if a.mode == modeCommand {
		a.mode = modeChat
		a.status = ""
	} else {
		a.mode = modeCommand
		a.focus = focusChat
		a.status = "term mode — enter runs the line in the shell, esc back"
	}
}

func (a *App) dismissNewestChip() {
	_, _, c, ok := a.chat.NewestPending()
	if !ok {
		a.status = "no proposed command"
		return
	}
	c.State = ChipDismissed
	a.status = "dismissed proposed command"
}

// ---- mouse routing ----

func (a *App) onClick(e uv.MouseClickEvent) {
	m := e.Mouse()
	if a.modal != nil {
		a.modalAction(a.modal.click(m.X, m.Y))
		return
	}
	if a.help {
		a.help = false
		return
	}
	if a.shell == nil {
		return
	}
	for _, h := range a.hits {
		if inRect(h.rect, m.X, m.Y) {
			switch h.kind {
			case hitRun:
				a.runChip(h.chip)
			case hitEdit:
				a.editChip(h.chip)
			case hitDismiss:
				a.dismissChip(h.chip)
			case hitSettings:
				a.modal = newSettingsModal(a.cfg)
			}
			return
		}
	}
	if inRect(a.rectChat, m.X, m.Y) {
		a.focus = focusChat
		return
	}
	if inRect(a.rectTerm, m.X, m.Y) {
		a.focus = focusTerminal
		if a.shell.Snapshot().ChildMouse {
			a.forwardMouse(e, a.rectTerm)
			return
		}
		switch m.Button {
		case uv.MouseLeft:
			relX := m.X - a.rectTerm.Min.X
			vl := a.vlineAt(a.rectTerm, m.Y)
			if m.Mod&uv.ModShift != 0 && a.sel.have {
				a.sel.v1 = vl
				a.sel.x1 = relX
				a.sel.dragging = true
			} else {
				a.sel = selState{dragging: true, have: true, v0: vl, v1: vl, x0: relX, x1: relX}
			}
		case uv.MouseRight:
			a.attachSelection()
		}
	}
}

func (a *App) onMotion(e uv.MouseMotionEvent) {
	if a.modal != nil || a.help || a.shell == nil {
		return
	}
	m := e.Mouse()
	if inRect(a.rectTerm, m.X, m.Y) {
		if a.shell.Snapshot().ChildMouse {
			a.forwardMouse(e, a.rectTerm)
			return
		}
		if a.sel.dragging {
			a.sel.v1 = a.vlineAt(a.rectTerm, m.Y)
			a.sel.x1 = clamp(m.X-a.rectTerm.Min.X, 0, max(0, a.rectTerm.Dx()-1))
		}
	}
}

func (a *App) onRelease(e uv.MouseReleaseEvent) {
	if a.shell == nil {
		return
	}
	m := e.Mouse()
	if inRect(a.rectTerm, m.X, m.Y) {
		if a.shell.Snapshot().ChildMouse {
			a.forwardMouse(e, a.rectTerm)
			return
		}
		a.sel.dragging = false
	}
}

func (a *App) onWheel(e uv.MouseWheelEvent) {
	if a.modal != nil || a.help || a.shell == nil {
		return
	}
	m := e.Mouse()
	if inRect(a.rectTerm, m.X, m.Y) {
		if a.shell.Snapshot().ChildMouse && m.Mod&uv.ModShift == 0 {
			a.forwardMouse(e, a.rectTerm)
			return
		}
		if e.Button == uv.MouseWheelUp {
			a.termScroll += 3
		} else {
			a.termScroll -= 3
		}
		if a.termScroll < 0 {
			a.termScroll = 0
		}
	} else if inRect(a.rectChat, m.X, m.Y) {
		if e.Button == uv.MouseWheelUp {
			a.chat.ScrollBy(3)
		} else {
			a.chat.ScrollBy(-3)
		}
	}
}

// forwardMouse translates host coords to child coords and encodes the
// event into the pty (only reaches the child if it enabled mouse mode).
func (a *App) forwardMouse(ev uv.MouseEvent, rect uv.Rectangle) {
	m := ev.Mouse()
	rel := uv.Mouse{
		X:      clamp(m.X-rect.Min.X, 0, max(0, rect.Dx()-1)),
		Y:      clamp(m.Y-rect.Min.Y, 0, max(0, rect.Dy()-1)),
		Button: m.Button,
		Mod:    m.Mod,
	}
	switch ev.(type) {
	case uv.MouseClickEvent:
		a.shell.SendMouse(uv.MouseClickEvent(rel))
	case uv.MouseReleaseEvent:
		a.shell.SendMouse(uv.MouseReleaseEvent(rel))
	case uv.MouseMotionEvent:
		a.shell.SendMouse(uv.MouseMotionEvent(rel))
	case uv.MouseWheelEvent:
		a.shell.SendMouse(uv.MouseWheelEvent(rel))
	}
}

func (a *App) onPaste(e uv.PasteEvent) {
	if a.modal != nil || a.help || a.shell == nil {
		return
	}
	if a.focus == focusTerminal {
		a.shell.Paste(e.Content)
	} else {
		a.activeInput().Insert(e.Content)
	}
}

// ---- chips ----

func (a *App) runChip(c *Chip) {
	if a.shell == nil {
		return
	}
	a.shell.Run(c.Cmd)
	c.State = ChipRun
	a.status = "executed: " + truncate(a.scr, c.Cmd, 40)
}

func (a *App) editChip(c *Chip) {
	a.mode = modeCommand
	a.cmdInput.SetText(c.Cmd)
	a.focus = focusChat
	a.status = "editing command — enter runs it, esc back to chat"
}

func (a *App) dismissChip(c *Chip) {
	c.State = ChipDismissed
}

func (a *App) runNewestChip() {
	_, _, c, ok := a.chat.NewestPending()
	if !ok {
		a.status = "no proposed command"
		return
	}
	a.runChip(c)
}

func (a *App) editNewestChip() {
	_, _, c, ok := a.chat.NewestPending()
	if !ok {
		a.status = "no proposed command"
		return
	}
	a.editChip(c)
}

func (a *App) attachSelection() {
	text := a.selectionText()
	if text == "" {
		a.status = "nothing selected — drag to select terminal text"
		return
	}
	a.selCtx = text
	a.status = fmt.Sprintf("context attached (%d chars) — next message includes it", len(text))
}

// ---- chat / AI ----

func (a *App) sendChat() {
	text := strings.TrimSpace(a.input.Text())
	if text == "" {
		return
	}
	if a.cfg.Model == "" {
		a.status = "no model configured"
		a.modal = newSettingsModal(a.cfg)
		return
	}
	a.input.PushHistory(text)
	a.input.Clear()
	sel := a.selCtx
	a.selCtx = ""
	a.chat.AddUser(text)
	msgs := a.buildMessages(sel)
	a.streaming = true
	a.canceled = false
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.chat.AddAssistant()
	a.chat.ScrollBy(-1 << 30) // follow

	model, temp := a.cfg.Model, a.cfg.Temperature
	client := a.client
	go func() {
		ch, err := client.Stream(ctx, model, temp, msgs)
		if err != nil {
			a.term.SendEvent(streamEndEvent{err: err})
			return
		}
		for d := range ch {
			if d.Err != nil {
				a.term.SendEvent(streamEndEvent{err: d.Err})
				return
			}
			if d.Text != "" {
				a.term.SendEvent(streamDeltaEvent{text: d.Text})
			}
			if d.Final {
				break
			}
		}
		a.term.SendEvent(streamEndEvent{})
	}()
}

func (a *App) sendCommand() {
	text := a.cmdInput.Text()
	if strings.TrimSpace(text) == "" {
		a.mode = modeChat
		a.status = ""
		return
	}
	a.cmdInput.PushHistory(text)
	a.cmdInput.Clear()
	if a.shell != nil {
		a.shell.Run(text)
	}
	a.status = ""
}

func (a *App) onStreamDelta(text string) {
	a.chat.AppendStream(text)
	a.chat.SyncChips()
	a.spin++
}

func (a *App) onStreamEnd(err error) {
	if a.canceled {
		a.canceled = false
		a.streaming = false
		a.cancel = nil
		return
	}
	a.streaming = false
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.chat.FinishStream()
	if err != nil {
		a.chat.AddNote("⚠ " + err.Error())
		a.status = "stream error"
		return
	}
	a.chat.SyncChips()
	if a.cfg.AutoRun {
		pending := a.chat.PendingChips()
		if len(pending) > 0 {
			cmds := make([]string, 0, len(pending))
			for _, c := range pending {
				c.State = ChipRun
				cmds = append(cmds, c.Cmd)
			}
			go func() {
				for _, c := range cmds {
					if a.shell != nil {
						a.shell.Run(c)
					}
					time.Sleep(400 * time.Millisecond)
				}
			}()
			a.status = fmt.Sprintf("auto-run: %d command(s) sent", len(cmds))
		}
	}
}

func (a *App) cancelStream() {
	if a.cancel != nil {
		a.cancel()
	}
	a.streaming = false
	a.canceled = true
	a.chat.FinishStream()
	a.chat.AddNote("✕ canceled")
	a.status = ""
}

func (a *App) buildMessages(sel string) []ai.Message {
	var b strings.Builder
	b.WriteString("You are the AI assistant embedded in \"con\", a terminal emulator with a chat side panel.\n")
	b.WriteString("The user runs a real shell in the terminal pane; the recent terminal output is included below.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Be concise. Answer directly.\n")
	b.WriteString("- To propose a shell command, output a fenced code block marked `bash`, one command per block:\n\n")
	b.WriteString("  ```bash\n  command here\n  ```\n\n")
	b.WriteString("- Never put prose inside a code block; explain around it instead.\n")
	b.WriteString("- The user reviews every proposed command before running it; they may edit or reject it.\n")
	b.WriteString("- If several commands are needed, propose them in order as separate blocks.\n")
	b.WriteString("- Use the terminal output below as context; don't ask the user to re-paste it.\n")

	shellName := strings.TrimSpace(a.shellPath)
	if i := strings.LastIndex(shellName, "/"); i >= 0 {
		shellName = shellName[i+1:]
	}
	env := fmt.Sprintf("OS: %s/%s · shell: %s", runtime.GOOS, runtime.GOARCH, shellName)
	if a.shell != nil {
		if cwd := a.shell.Snapshot().Cwd; cwd != "" {
			env += " · cwd: " + cwd
		}
	}
	b.WriteString("\nEnvironment: " + env + "\n")

	if a.shell != nil {
		if tail := a.terminalTail(a.cfg.ContextLines); tail != "" {
			b.WriteString(fmt.Sprintf("\nRecent terminal output (last %d lines):\n", a.cfg.ContextLines))
			b.WriteString(tail)
			b.WriteString("\n")
		}
	}
	if sel != "" {
		b.WriteString("\nThe user selected this text in the terminal:\n<<<\n")
		b.WriteString(sel)
		b.WriteString("\n>>>\n")
	}
	if extra := strings.TrimSpace(a.cfg.SystemPrompt); extra != "" {
		b.WriteString("\nAdditional instructions from the user:\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}

	msgs := []ai.Message{{Role: "system", Content: b.String()}}
	msgs = append(msgs, a.chat.History(24)...)
	return msgs
}

func (a *App) onModels(models []string, err error) {
	if a.modal != nil {
		a.modal.setModels(models, err)
		return
	}
	if err != nil {
		a.status = "AI offline — alt+s to configure"
		return
	}
	if a.cfg.Model == "" && len(models) > 0 {
		a.cfg.Model = models[0]
		if err := config.Save(a.cfgPath, a.cfg); err == nil {
			a.status = "model: " + a.cfg.Model + " (saved)"
		} else {
			a.status = "model: " + a.cfg.Model
		}
		return
	}
	a.status = "ready · model: " + a.cfg.Model
}

// ---- settings ----

func (a *App) modalAction(act modalAction) {
	switch act {
	case actSave:
		if err := a.modal.apply(a.cfg); err != nil {
			a.modal.err = err.Error()
			return
		}
		if err := config.Save(a.cfgPath, a.cfg); err != nil {
			a.modal.err = err.Error()
			return
		}
		a.client = ai.New(a.cfg.BaseURL, a.cfg.APIKey)
		a.modal = nil
		a.status = "settings saved"
	case actCancel:
		a.modal = nil
	case actFetchModels:
		go a.probeModels()
	}
}

// ---- rendering ----

// Draw renders the whole UI into area (implements uv.Drawable).
func (a *App) Draw(scr uv.Screen, area uv.Rectangle) {
	a.hits = a.hits[:0]
	a.layout(area)

	if a.shell == nil {
		fillRect(scr, area, uv.Style{})
		msg := "starting…"
		if a.startErr != nil {
			msg = "shell failed: " + a.startErr.Error()
		}
		w := min(len(msg)+4, area.Dx()-2)
		rect := uv.Rect(area.Min.X+(area.Dx()-w)/2, area.Min.Y+area.Dy()/2, w, 3)
		inner := drawBox(scr, rect, stBorder, "")
		putStr(scr, inner, 1, 0, truncate(scr, msg, inner.Dx()-2), stDim)
		return
	}

	// Terminal pane.
	cursor, showCursor := a.drawTerminal(scr, a.rectTerm)

	// Chat pane.
	border := stBorder
	if a.focus == focusChat {
		border = stAccent
	}
	chatInner := drawBox(scr, a.rectChat, border, "chat")
	a.drawChatHeader(scr, chatInner, &a.hits)
	a.chat.DrawBody(scr, a.rectChatBody, &a.hits)

	prefix, pst := "chat ❯", stPurple
	if a.mode == modeCommand {
		prefix, pst = "term ❯", stGreen
	}
	inputCursor := a.activeInput().Draw(scr, a.rectChatInput, prefix, pst, a.focus == focusChat)

	a.drawStatus(scr, a.rectStatus)

	if a.modal != nil {
		a.modal.draw(scr, area)
	}
	if a.help {
		a.drawHelp(scr, area)
	}

	// Host cursor.
	switch {
	case a.modal != nil || a.help:
		a.scr.HideCursor()
	case a.focus == focusChat:
		a.scr.SetCursorPosition(inputCursor.X, inputCursor.Y)
		a.scr.ShowCursor()
	case showCursor:
		a.scr.SetCursorPosition(cursor.X, cursor.Y)
		a.scr.ShowCursor()
	default:
		a.scr.HideCursor()
	}
}

func (a *App) layout(area uv.Rectangle) {
	w, h := area.Dx(), area.Dy()
	a.rectStatus = uv.Rect(0, h-1, w, 1)
	main := uv.Rect(0, 0, w, h-1)

	chatW := int(float64(w)*a.cfg.ChatRatio + 0.5)
	chatW = clamp(chatW, 24, max(24, w-16))
	if w < 56 {
		chatW = max(18, w*2/5)
	}
	a.rectChat = uv.Rect(max(0, w-chatW), main.Min.Y, min(chatW, w), main.Dy())
	a.rectTerm = uv.Rect(0, main.Min.Y, max(0, w-chatW), main.Dy())

	// Chat box interior: header (1) + body + input bar (3).
	inner := uv.Rect(a.rectChat.Min.X+1, a.rectChat.Min.Y+1, max(0, a.rectChat.Dx()-2), max(0, a.rectChat.Dy()-2))
	a.rectChatBody = uv.Rect(inner.Min.X, inner.Min.Y+1, inner.Dx(), max(0, inner.Dy()-4))
	a.rectChatInput = uv.Rect(inner.Min.X, inner.Max.Y-3, inner.Dx(), 3)
}

func (a *App) drawChatHeader(scr uv.Screen, inner uv.Rectangle, hits *[]hit) {
	if inner.Dy() < 1 {
		return
	}
	model := a.cfg.Model
	st := stGreen
	if model == "" {
		model = "no model — click or ctrl+s"
		st = stRed
	}
	x := putStr(scr, inner, 1, 0, "◍ "+model, st)
	// The model name is a clickable shortcut to settings.
	if w := scrWidth(scr, "◍ "+model); w > 0 {
		*hits = append(*hits, hit{
			rect: uv.Rect(inner.Min.X+1, inner.Min.Y, min(w, inner.Dx()-1), 1),
			kind: hitSettings,
		})
	}
	if a.streaming {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		putStr(scr, inner, x+2, 0, frames[a.spin%len(frames)]+" streaming", stYellow)
	} else if a.selCtx != "" {
		putStr(scr, inner, x+2, 0, fmt.Sprintf("✂ ctx %d chars", len(a.selCtx)), stYellow)
	}
}

func (a *App) drawStatus(scr uv.Screen, rect uv.Rectangle) {
	fillRect(scr, rect, stReverse)
	if rect.Dx() < 10 {
		return
	}

	// left: focus + terminal title/cwd
	focus := "term"
	if a.focus == focusChat {
		focus = "chat"
	}
	x := putStr(scr, rect, 1, 0, "["+focus+"]", stReverse)

	snap := a.shell.Snapshot()
	label := snap.Title
	if label == "" {
		label = snap.Cwd
	}
	if home := homeDir(); label != "" && home != "" && strings.HasPrefix(label, home) {
		label = "~" + label[len(home):]
	}
	if label != "" {
		x = putStr(scr, rect, x+1, 0, truncate(scr, label, max(0, rect.Dx()/2)), uv.Style{Attrs: uv.AttrReverse | uv.AttrFaint})
	}
	if a.termScroll > 0 {
		putStr(scr, rect, x+2, 0, fmt.Sprintf("↑%d", a.termScroll), stYellow)
	}

	// right: state
	var right string
	switch {
	case a.status != "":
		right = a.status
	case a.streaming:
		right = "streaming…"
	case a.focus == focusTerminal:
		right = "ctrl+g → chat · alt+h help"
	default:
		right = "ctrl+s settings · ctrl+t term · ctrl+g help"
	}
	putStr(scr, rect, max(1, rect.Dx()-2-scrWidth(scr, right)), 0, right, uv.Style{Attrs: uv.AttrReverse | uv.AttrBold})
}

func scrWidth(scr uv.Screen, s string) int {
	return scr.WidthMethod().StringWidth(s)
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
