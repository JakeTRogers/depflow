package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMainRunsVersionCommand(t *testing.T) {
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() stdout error = %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() stderr error = %v", err)
	}

	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	os.Args = []string{"depflow", "version"}

	main()

	if err := stdoutWriter.Close(); err != nil {
		t.Fatalf("stdoutWriter.Close() error = %v", err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatalf("stderrWriter.Close() error = %v", err)
	}

	stdout := readAll(t, stdoutReader)
	stderr := readAll(t, stderrReader)
	if !strings.Contains(stdout, "depflow ") {
		t.Fatalf("stdout = %q, want version output", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func readAll(t *testing.T, reader *os.File) string {
	t.Helper()

	defer func() {
		if err := reader.Close(); err != nil {
			t.Fatalf("reader.Close() error = %v", err)
		}
	}()

	buffer := &bytes.Buffer{}
	if _, err := io.Copy(buffer, reader); err != nil {
		t.Fatalf("io.Copy() error = %v", err)
	}

	return buffer.String()
}
