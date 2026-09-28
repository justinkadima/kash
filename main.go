// kash — a portable terminal with an integrated AI side panel.
//
// It runs your shell in a pty inside the left pane and chats with an
// OpenAI-compatible server (Ollama, llama.cpp, LM Studio, …) in the right
// pane. The model sees recent terminal output, can receive selected text
// as context, and proposes commands that you run, edit, or dismiss.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/justinkadima/kash/internal/config"
	"github.com/justinkadima/kash/internal/shell"
	"github.com/justinkadima/kash/internal/ui"
)

func main() {
	var (
		cfgPath   = flag.String("config", "", "config file path (default: $KASH_CONFIG or user config dir)")
		shellPath = flag.String("shell", "", "shell to run (default: $SHELL)")
	)
	flag.Parse()

	path, err := config.Path(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kash: resolving config path: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kash: %v\n", err)
		os.Exit(1)
	}

	if err := ui.Run(cfg, path, shell.Detect(*shellPath)); err != nil {
		fmt.Fprintf(os.Stderr, "kash: %v\n", err)
		os.Exit(1)
	}
}
