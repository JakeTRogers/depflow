package progress

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestFromCountAndLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		count     int
		wantVerb  Verbosity
		wantLevel slog.Level
	}{
		{"negative", -1, Quiet, slog.LevelWarn},
		{"zero", 0, Quiet, slog.LevelWarn},
		{"one", 1, Info, slog.LevelInfo},
		{"two", 2, Debug, slog.LevelDebug},
		{"three", 3, Trace, levelTrace},
		{"four clamped", 4, Trace, levelTrace},
		{"ten clamped", 10, Trace, levelTrace},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := FromCount(tc.count)
			if v != tc.wantVerb {
				t.Errorf("FromCount(%d) = %d, want %d", tc.count, v, tc.wantVerb)
			}
			if got := v.level(); got != tc.wantLevel {
				t.Errorf("Verbosity(%d).level() = %v, want %v", tc.count, got, tc.wantLevel)
			}
		})
	}
}

func TestNewLoggerQuietShowsWarningsAndErrorsOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewLogger(&buf, Quiet)

	log.Info("should not appear")
	log.Warn("visible warning")
	log.Error("visible error")

	out := buf.String()
	if contains(out, "should not appear") {
		t.Errorf("info message visible in quiet mode: %q", out)
	}
	if !contains(out, "visible warning") {
		t.Errorf("warning message missing in quiet mode: %q", out)
	}
	if !contains(out, "visible error") {
		t.Errorf("error message missing in quiet mode: %q", out)
	}
}

func TestNewLoggerInfoShowsInfoOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewLogger(&buf, Info)

	log.Info("visible info")
	log.Debug("hidden debug")

	out := buf.String()
	if len(out) == 0 {
		t.Fatal("info logger produced no output")
	}
	if !contains(out, "visible info") {
		t.Errorf("missing info message in output: %q", out)
	}
	if contains(out, "hidden debug") {
		t.Errorf("debug message visible at info level: %q", out)
	}
}

func TestNewLoggerDebugShowsInfoAndDebug(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewLogger(&buf, Debug)

	log.Info("info msg")
	log.Debug("debug msg")
	log.Log(context.Background(), levelTrace, "trace msg")

	out := buf.String()
	if !contains(out, "info msg") {
		t.Errorf("missing info message: %q", out)
	}
	if !contains(out, "debug msg") {
		t.Errorf("missing debug message: %q", out)
	}
	if contains(out, "trace msg") {
		t.Errorf("trace message visible at debug level: %q", out)
	}
}

func TestNewLoggerTraceShowsAll(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewLogger(&buf, Trace)

	log.Info("info msg")
	log.Debug("debug msg")
	log.Log(context.Background(), levelTrace, "trace msg")

	out := buf.String()
	if !contains(out, "info msg") {
		t.Errorf("missing info message: %q", out)
	}
	if !contains(out, "debug msg") {
		t.Errorf("missing debug message: %q", out)
	}
	if !contains(out, "trace msg") {
		t.Errorf("missing trace message: %q", out)
	}
}

func TestTrackerLifecycle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tr := NewTracker(&buf, 3)

	tr.SetStatus("step 1")
	tr.Increment()
	tr.SetStatus("step 2")
	tr.Increment()
	tr.SetStatus("done")
	tr.Increment()

	tr.Stop()
	// calling Stop again should not panic
	tr.Stop()
}

func TestTrackerLogWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tr := NewTracker(&buf, 1)

	w := tr.LogWriter()
	if w == nil {
		t.Fatal("LogWriter() returned nil")
	}

	// Verify the writer accepts data without error.
	// We don't assert the buffer content because mpb controls rendering timing.
	n, err := w.Write([]byte("test line\n"))
	if err != nil {
		t.Fatalf("LogWriter().Write() error: %v", err)
	}
	if n != len("test line\n") {
		t.Errorf("LogWriter().Write() = %d, want %d", n, len("test line\n"))
	}

	tr.Increment()
	tr.Stop()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && bytes.Contains([]byte(s), []byte(substr))
}
