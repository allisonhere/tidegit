package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidegit/internal/config"
	"github.com/allisonhere/tidegit/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	themeFlag := flag.String("theme", "",
		"theme name for this session, or \"match-omarchy\"; overrides the config file")
	configFlag := flag.String("config", "",
		"path to config.toml; defaults to the XDG config directory")
	flag.Parse()
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "Usage: tidegit [-theme NAME] [-config PATH] [PATH]")
		os.Exit(2)
	}

	primary := *configFlag
	if primary == "" {
		primary = config.Path()
	} else {
		// A custom config path keeps its app-managed overrides beside it.
		primary, _ = filepath.Abs(primary)
	}
	overrides := config.OverridesPath()
	if *configFlag != "" {
		overrides = filepath.Join(filepath.Dir(primary), "overrides.toml")
	}
	store := config.Open(primary, overrides)
	for _, warning := range append(store.Errors(), store.Warnings()...) {
		fmt.Fprintln(os.Stderr, "tidegit config:", warning)
	}
	if *themeFlag != "" {
		if _, ok := ui.ResolveTheme(*themeFlag); !ok {
			fmt.Fprintln(os.Stderr, "Unknown theme:", *themeFlag)
			fmt.Fprintln(os.Stderr, "Available:", strings.Join(ui.ThemeNames(), ", "))
			os.Exit(2)
		}
		// A command-line theme is a session override, not persisted.
		_ = store.Override("appearance.theme", *themeFlag)
	}

	statePath := config.StatePath()
	state, stateErr := config.LoadState(statePath)
	if stateErr != nil {
		fmt.Fprintln(os.Stderr, "tidegit state:", stateErr)
	}
	cfg := store.Config()

	path := "."
	switch {
	case flag.NArg() == 1:
		path = flag.Arg(0)
	case cfg.Behavior.RestoreLastRepository && state.LastRepository != "":
		if _, err := os.Stat(state.LastRepository); err == nil {
			path = state.LastRepository
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	options := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.Behavior.Mouse {
		options = append(options, tea.WithMouseCellMotion())
	}
	_, err := tea.NewProgram(ui.NewConfigured(ctx, path, store, state, statePath), options...).Run()
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
