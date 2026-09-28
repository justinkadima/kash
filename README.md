# con

A portable terminal with an AI side panel. One Go binary, no runtime, no
installer — your shell in the left pane, a chat backed by any
OpenAI-compatible server (Ollama, llama.cpp, LM Studio, vLLM, OpenRouter…)
in the right.

The model sees what your terminal sees: recent output is injected as
context automatically, you can attach any selected text with a right-click,
and commands the model proposes show up as **chips** — you run, edit, or
dismiss them. Nothing executes without you.

```
┌─────────────────────────────┬──────────────────┐
│                             │ ◍ llama3.1:8b     │
│  your shell (real pty)      │ ❯ you            │
│                             │ free disk?       │
│  $ df -h                   │                  │
│  ...                       │ ◍ assistant      │
│                             │ ╭─ cmd ────────╮ │
│                             │ │ df -h        │ │
│                             │ ╰──────────────╯ │
│                             │ ▸ run ✎ edit ✕ x │
├─────────────────────────────┤ chat ❯           │
│ [term] ~          alt+h help│──────────────────│
└─────────────────────────────┴──────────────────┘
```

## Build

```sh
go build -o con .
```

Pure Go (no CGO); cross-compiles. macOS and Linux are exercised; Windows
should work via ConPTY but is untested.

## Run

```sh
./con
```

Ollama at `http://127.0.0.1:11434` is picked up automatically (first model
in `/v1/models`). Point it elsewhere with `alt+s` → settings, or the config
file.

### Config

`$CON_CONFIG` → else `~/.config/con/config.json`
(`~/Library/Application Support/con/config.json` on macOS). For a truly
portable setup, keep the binary next to a config and run:

```sh
CON_CONFIG=./config.json ./con
```

```json
{
  "base_url": "http://127.0.0.1:11434",
  "api_key": "",
  "model": "llama3.1:8b",
  "temperature": 0.7,
  "context_lines": 60,
  "auto_run": false,
  "chat_ratio": 0.38,
  "system_prompt": ""
}
```

- `context_lines` — how many lines of terminal scrollback ride along with
  each message
- `auto_run` — **danger**: execute proposed commands as soon as the
  response completes (off by default; the shell is unsandboxed)
- `api_key` — sent as `Authorization: Bearer …` for servers that want one
  (ignored by Ollama)

## Keys

Every action has a **Ctrl binding** — raw control codes that every
terminal delivers, no configuration needed — plus an Alt binding for
terminals with proper meta support.

| key (ctrl / alt) | action |
|---|---|
| `ctrl+g` / `alt+space`·click | focus: terminal → chat · chat → help |
| `ctrl+t` / `alt+t` | toggle chat ⇄ **term input** (type a command, enter runs it) |
| `enter` | chat: send message · term input: run line |
| `ctrl+s` / `alt+s` | settings (also: click the model name) |
| `ctrl+r` / `alt+r` | run the newest proposed command chip |
| `alt+e` | edit the newest chip (loads it into term input) |
| `ctrl+d` / `alt+d` | dismiss the newest chip |
| `ctrl+l` / `alt+c` | clear conversation |
| `ctrl+q` / `alt+q` | quit (from chat input) |
| drag / right-click | select terminal text / attach selection as context |
| `alt+a` | attach selection as context |
| `ctrl+c` | chat: cancel stream · terminal: SIGINT to shell |
| `esc` | cancel stream · leave term input |
| wheel / `pgup`/`pgdn` | scroll terminal / chat |

Chip buttons are clickable too: **run · edit · dismiss**.

## @references

Type `@` in the chat input to reference content in that message:

- `@selection` — the current terminal selection (what you dragged)
- `@path/to/file` — file content (capped at 8 KB / 400 lines)
- `@dir/` — a directory listing

A popup lists candidates as you type: `↑↓` navigate · `tab` completes ·
`esc` closes. Relative paths resolve against the shell's cwd when the
shell reports it (OSC 7), else con's cwd. References are inserted beneath
your message for that turn only (visible to the model as `--- @… ---`
blocks), so they don't bloat later turns. Emails (`foo@bar.com`) are
ignored; a token must stand alone after whitespace.

The right-click / `alt+a` attach-selection flow still works too — it pins
the selection to your next message without typing anything.

### macOS: if Alt keys do nothing

Terminal.app and iTerm2 compose special characters with Option by
default (Option+R → `®`), so the app never receives them. Either use the
Ctrl bindings above, or enable meta mode for the terminal profile:

- **Terminal.app:** Settings → Profiles → Keyboard → *Use Option as Meta key*
- **iTerm2:** Settings → Profiles → Keys → *Option key sends: Esc+*
- Ghostty, kitty, WezTerm, tmux send Alt natively

Open help (`ctrl+g` from chat) — it shows the **last key your terminal
delivered**, which tells you immediately what is reaching the app.

## How it works

- `creack/pty` runs your `$SHELL` in a pty
- `charmbracelet/x/vt` is the terminal emulator: it parses the child's
  output into a cell grid (with scrollback), renders into
  `charmbracelet/ultraviolet`'s screen, and encodes your keystrokes/mouse
  back into the child — full-screen apps (vim, htop) work
- chat streams over SSE from `{base_url}/v1/chat/completions`
- each request carries: a system prompt, the last `context_lines` of
  scrollback, any attached selection, and the conversation
- fenced ```bash blocks in responses become chips; running one types the
  command into the pty — exactly as if you pressed enter yourself
- one lock serializes emulator reads/writes between the pty pump and the
  render loop; the AI request runs on its own goroutine and posts events
  back to the UI loop

## Security

The model can only propose. Chips run in your real shell with your
permissions — read them before running. `auto_run` removes the review
step; don't enable it on machines you care about.