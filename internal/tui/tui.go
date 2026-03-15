package tui

import (
	"context"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"fuse/internal/domain"
)

type Client interface {
	Status(ctx context.Context) (domain.ClusterStatus, error)
	Nodes(ctx context.Context) ([]domain.Node, []domain.Device, error)
	Jobs(ctx context.Context) ([]domain.Job, error)
	Events(ctx context.Context, limit int) ([]domain.Event, error)
}

type Options struct {
	RefreshInterval time.Duration
	SourceLabel     string
	EventLimit      int
	Title           string
	MinWidth        int
	MinHeight       int
}

func Run(ctx context.Context, out io.Writer, cli Client, opts Options) error {
	model := newModel(ctx, cli, normalizeOptions(opts))
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithOutput(out),
	)
	if _, err := program.Run(); err != nil {
		return err
	}
	return nil
}

func normalizeOptions(opts Options) Options {
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 2 * time.Second
	}
	if opts.SourceLabel == "" {
		opts.SourceLabel = "live"
	}
	if opts.EventLimit <= 0 {
		opts.EventLimit = 10
	}
	if opts.Title == "" {
		opts.Title = "Fuse Ops"
	}
	if opts.MinWidth <= 0 {
		opts.MinWidth = 68
	}
	if opts.MinHeight <= 0 {
		opts.MinHeight = 18
	}
	return opts
}
