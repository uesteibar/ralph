package commands

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/tui"
)

// Tui opens the multi-workspace overview TUI.
func Tui(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	infos, err := tui.LoadWorkspaceInfos(cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("loading workspaces: %w", err)
	}

	model := tui.NewMultiModel(infos)
	model.SetMakeStopFn(func(wsPath string) func() {
		return func() { stopDaemon(wsPath) }
	})
	model.SetMakeResumeFn(func(index int, wsName, wsPath string) tea.Cmd {
		return func() tea.Msg {
			_, err := spawnDaemonFn(wsName, loop.DefaultMaxIterations)
			if err != nil {
				return tui.MakeMultiDaemonResumedMsg(index, err)
			}
			err = waitForPIDFileFn(wsPath, 5*time.Second)
			return tui.MakeMultiDaemonResumedMsg(index, err)
		}
	})
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start log readers for each workspace and forward events to the TUI.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i, ws := range infos {
		logsDir := filepath.Join(ws.WsPath, "logs")
		idx := i
		reader := events.NewLogReader(logsDir)
		go reader.Run(ctx)
		go func() {
			for evt := range reader.Events() {
				p.Send(tui.MakeMultiLogEventMsg(idx, evt))
			}
		}()
	}

	// Monitor daemon liveness for running workspaces.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				for i := range infos {
					running := runstate.IsRunning(infos[i].WsPath)
					if running != infos[i].Running {
						infos[i].Running = running
					}
				}
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}
