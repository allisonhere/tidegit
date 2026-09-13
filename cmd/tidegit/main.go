package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/allisonhere/tidegit/internal/ui"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	themeName := flag.String("theme", "catppuccin-mocha", "TideUI theme name")
	flag.Parse()
	theme, ok := tideui.ThemeByName(*themeName)
	if !ok {
		fmt.Fprintln(os.Stderr, "Unknown TideUI theme:", *themeName)
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
