// Package clip copies text to the operating system clipboard by shelling
// out to the platform utility. This keeps the binary CGO-free and statically
// linkable; exec-ing pbcopy/xclip/clip is the same trick atotto/clipboard
// and most Go TUIs use.
package clip

import (
	"os/exec"
	"runtime"
	"strings"
)

// Copy writes s to the system clipboard. On Linux it tries the Wayland
// utility first, then the X11 ones; an error means no supported utility
// was found (e.g. a headless box).
func Copy(s string) error {
	switch runtime.GOOS {
	case "darwin":
		return pipe("pbcopy", s)
	case "windows":
		s = strings.ReplaceAll(s, "\n", "\r\n")
		return pipe("clip", s)
	default:
		if err := pipe("wl-copy", s); err == nil {
			return nil
		}
		if err := pipe("xclip", "-selection", "clipboard", s); err == nil {
			return nil
		}
		return pipe("xsel", "--clipboard", "--input", s)
	}
}

// pipe runs the clipboard utility, feeding it s on stdin.
func pipe(name string, s string, args ...string) error {
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := stdin.Write([]byte(s)); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		return err
	}
	return cmd.Wait()
}