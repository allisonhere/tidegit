package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"strings"

	"github.com/allisonhere/tidegit/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	themeName := flag.String("theme", "catppuccin-mocha",
		"theme name, or \"match-omarchy\" to follow the desktop theme")
	flag.Parse()
	theme, ok := ui.ResolveTheme(*themeName)
	if !ok {
		fmt.Fprintln(os.Stderr, "Unknown theme:", *themeName)
		fmt.Fprintln(os.Stderr, "Available:", strings.Join(ui.ThemeNames(), ", "))
		os.Exit(2)
	}
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "Usage: tidegit [-theme NAME] [PATH]")
		os.Exit(2)
	}
	path := "."
	if flag.NArg() == 1 {
		path = flag.Arg(0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := tea.NewProgram(ui.New(ctx, path, theme), tea.WithAltScreen()).Run()
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
