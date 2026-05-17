// Package tui implements an interactive terminal UI for autoswe built on
// Bubble Tea. The CLI launches it when invoked without a prompt; it runs a
// multi-turn conversation against an agent.Agent and persists each turn to
// the session JSONL file.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/russellhaering/autoswe/pkg/agent"
)

// Config is everything the CLI hands the TUI to run. AgentOptions should have
// Provider, Model, Tools, Policy, System, and MaxTurns set; InitialMessages is
// used as the starting history, and the TUI overrides it for each turn.
type Config struct {
	AgentOptions agent.Options
	SessionDir   string
	SessionID    string
}

// Run blocks until the user quits the TUI or ctx is cancelled.
func Run(ctx context.Context, cfg Config) error {
	m := newModel(ctx, cfg)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	_, err := prog.Run()
	return err
}
