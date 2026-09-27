package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// planEditor lets the user edit a plan file in place.
type planEditor interface {
	Edit(ctx context.Context, path string) error
}

// terminalEditor opens the user's editor attached to the controlling terminal.
type terminalEditor struct {
	getenv func(string) string
	goos   string
	stdin  *os.File
	stdout io.Writer
	stderr io.Writer
}

func newTerminalEditor() terminalEditor {
	return terminalEditor{
		getenv: os.Getenv,
		goos:   runtime.GOOS,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
}

func (e terminalEditor) Edit(ctx context.Context, path string) error {
	if !isTerminal(e.stdin) {
		return errors.New("--edit needs an interactive terminal; use `depflow plan -o FILE` and `depflow execute --plan FILE` instead")
	}

	args := editorCommand(e.getenv, e.goos)
	editor := exec.CommandContext(ctx, args[0], append(args[1:], path)...)
	editor.Stdin = e.stdin
	editor.Stdout = e.stdout
	editor.Stderr = e.stderr
	if err := editor.Run(); err != nil {
		return fmt.Errorf("running editor %q: %w", args[0], err)
	}

	return nil
}

// editorCommand resolves the editor like git does: $VISUAL, then $EDITOR, then a platform
// default. Values are split on whitespace so commands such as "code --wait" work.
func editorCommand(getenv func(string) string, goos string) []string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(getenv(name)); len(fields) > 0 {
			return fields
		}
	}

	if goos == "windows" {
		return []string{"notepad"}
	}
	return []string{"vi"}
}

func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
