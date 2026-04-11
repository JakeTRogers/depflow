// Package progress provides terminal progress tracking and verbosity-aware logging.
package progress

import (
	"io"
	"log/slog"
	"sync"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

const levelTrace slog.Level = -8

// Verbosity controls logger filtering for CLI progress output.
type Verbosity int

const (
	// Quiet shows warning and error log messages only.
	Quiet Verbosity = 0
	// Info shows info and higher-severity log messages.
	Info Verbosity = 1
	// Debug shows debug, info, and higher-severity log messages.
	Debug Verbosity = 2
	// Trace shows all log messages, including trace-level output.
	Trace Verbosity = 3
)

// FromCount converts a flag count to a verbosity level, clamping above 3 to Trace.
func FromCount(n int) Verbosity {
	if n <= 0 {
		return Quiet
	}
	if n >= 3 {
		return Trace
	}
	return Verbosity(n)
}

func (v Verbosity) level() slog.Level {
	switch v {
	case Quiet:
		return slog.LevelWarn
	case Info:
		return slog.LevelInfo
	case Debug:
		return slog.LevelDebug
	default:
		return levelTrace
	}
}

// NewLogger creates a logger configured for the given verbosity.
func NewLogger(w io.Writer, v Verbosity) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: v.level(),
	}))
}

type tracker struct {
	container *mpb.Progress
	bar       *mpb.Bar
	statusBar *mpb.Bar

	mu     sync.Mutex
	status string
	once   sync.Once
}

// Tracker provides progress display and log output coordination for the CLI.
type Tracker interface {
	SetStatus(status string)
	Increment()
	LogWriter() io.Writer
	Stop()
}

// NewTracker creates a progress tracker writing to out with the given total.
func NewTracker(out io.Writer, total int) Tracker {
	t := &tracker{}
	t.container = mpb.New(mpb.WithOutput(out))
	t.bar = t.container.AddBar(int64(total),
		mpb.PrependDecorators(
			decor.Name("progress ", decor.WCSyncSpaceR),
			decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
		),
	)
	t.statusBar = t.container.New(0,
		mpb.NopStyle(),
		mpb.PrependDecorators(
			decor.Any(func(_ decor.Statistics) string {
				t.mu.Lock()
				defer t.mu.Unlock()
				if t.status == "" {
					return ""
				}
				return "  " + t.status
			}),
		),
	)
	return t
}

// SetStatus updates the current status text shown on the bar.
func (t *tracker) SetStatus(status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status = status
}

// Increment advances the bar by one.
func (t *tracker) Increment() {
	t.bar.Increment()
}

// LogWriter returns a writer that routes output above the progress bar.
func (t *tracker) LogWriter() io.Writer {
	return t.container
}

// Stop finalizes the progress bar. It is safe to call multiple times.
func (t *tracker) Stop() {
	t.once.Do(func() {
		t.statusBar.Abort(false)
		t.bar.Abort(false)
		t.container.Wait()
	})
}
