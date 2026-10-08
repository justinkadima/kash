package ai

import (
	"regexp"
	"strings"
)

// Command is a shell command proposed by the model inside a fenced code
// block of the assistant message.
type Command struct {
	Lang string // normalized language tag, e.g. "bash", "" for untagged
	Text string // the command, trimmed
}

// fenceRe matches complete fenced code blocks. Incomplete blocks (still
// streaming) do not match, which is what we want: chips only appear once
// the model closes the fence.
var fenceRe = regexp.MustCompile("(?s)```([A-Za-z0-9_-]*)[^\n]*\n(.*?)```")

// commandLangs lists language tags we treat as shell commands. Untagged
// blocks are accepted too since local models often omit the tag.
var commandLangs = map[string]bool{
	"":        true,
	"bash":    true,
	"sh":      true,
	"shell":   true,
	"zsh":     true,
	"console": true,
}

// ExtractCommands returns every complete fenced block in text that looks
// like a shell command, in order of appearance.
func ExtractCommands(text string) []Command {
	var cmds []Command
	for _, m := range fenceRe.FindAllStringSubmatch(text, -1) {
		lang := strings.ToLower(strings.TrimSpace(m[1]))
		if !commandLangs[lang] {
			continue
		}
		body := strings.TrimRight(m[2], "\n")
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		cmds = append(cmds, Command{Lang: lang, Text: body})
	}
	return cmds
}

// IsCommandLang reports whether a fenced block's language tag marks a
// shell command (the fences chips are proposed from).
func IsCommandLang(lang string) bool {
	return commandLangs[strings.ToLower(strings.TrimSpace(lang))]
}

// StripCommands removes fenced command blocks from an assistant message,
// leaving only the prose (used for display in the chat pane).
func StripCommands(text string) string {
	return strings.TrimSpace(fenceRe.ReplaceAllString(text, "\x00"))
}
