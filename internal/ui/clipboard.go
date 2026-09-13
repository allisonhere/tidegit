package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// systemClipboard adapts platform tools to Ripple's asynchronous clipboard API.
// Commands are fixed argv; clipboard text is passed only over stdin.
type systemClipboard struct{}

func clipboardCommand(read bool) []string {
	if runtime.GOOS == "darwin" {
		if read {
			return []string{"pbpaste"}
		}
		return []string{"pbcopy"}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if read {
			return []string{"wl-paste", "--no-newline"}
		}
		return []string{"wl-copy"}
	}
	if os.Getenv("DISPLAY") != "" {
		if read {
			return []string{"xclip", "-selection", "clipboard", "-o"}
		}
		return []string{"xclip", "-selection", "clipboard", "-i"}
	}
	return nil
}
func (systemClipboard) Read() (string, error) {
	args := clipboardCommand(true)
	if args == nil {
		return "", fmt.Errorf("system clipboard unavailable; terminal paste still works")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, args[0], args[1:]...).Output()
	return string(b), err
}
func (systemClipboard) Write(text string) error {
	args := clipboardCommand(false)
	if args == nil {
		return fmt.Errorf("system clipboard unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
