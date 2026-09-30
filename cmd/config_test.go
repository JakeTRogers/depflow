// Tests for global and repository preference commands.
package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/config"
)

func TestConfigCommands(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DEPFLOW_MERGE_METHOD", "rebase")
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, test := range []struct {
		args []string
		want string
		fail bool
	}{
		{[]string{"config", "set", "merge-method", "squash"}, "Set merge-method = squash (global)", false},
		{[]string{"config", "set", "merge-method", "merge", "--repo", "git.example.com/Acme/Tool"}, "git.example.com/acme/tool", false},
		{[]string{"config", "get", "merge-method", "--repo", "git.example.com/acme/tool"}, "rebase", false},
		{[]string{"config", "show", "--show-origin", "--format", "json"}, "DEPFLOW_MERGE_METHOD", false},
		{[]string{"config", "show"}, "merge-method: rebase", false},
		{[]string{"config", "path"}, path, false},
		{[]string{"config", "reset"}, "requires --yes", true},
		{[]string{"config", "reset", "--yes"}, "global", false},
		{[]string{"config", "reset", "merge-method", "--repo", "git.example.com/acme/tool"}, "inherited", false},
		{[]string{"config", "set", "unknown", "merge"}, "unknown config key", true},
		{[]string{"config", "set", "merge-method", "bad"}, "invalid merge method", true},
		{[]string{"config", "get", "unknown"}, "unknown config key", true},
		{[]string{"config", "get", "merge-method", "--repo", "owner/repo", "--global"}, "choose only one", true},
		{[]string{"config", "show", "--format", "bad"}, "unsupported output format", true},
	} {
		root := newRootCommand(commandDeps{})
		out := &bytes.Buffer{}
		root.SetOut(out)
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append([]string{"--config", path}, test.args...))
		err := root.Execute()
		if (err != nil) != test.fail {
			t.Fatalf("%v: %v", test.args, err)
		}
		text := out.String()
		if err != nil {
			text += err.Error()
		}
		if !strings.Contains(text, test.want) {
			t.Fatalf("%v: %q, want %q", test.args, text, test.want)
		}
	}
}

func TestConfigCompletion(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"__complete", "config", "set", ""}, {"__complete", "config", "set", "merge-method", ""}} {
		root := newRootCommand(commandDeps{})
		out := &bytes.Buffer{}
		root.SetOut(out)
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "merge") {
			t.Fatalf("completion = %q", out.String())
		}
	}
}

func TestConfigEdit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, content string
		editorErr     error
		wantError     bool
	}{
		{"valid", "merge-method: squash\n", nil, false},
		{"invalid", "merge-method: bad\n", nil, true},
		{"cancelled", "", errors.New("editor cancelled"), true},
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		editor := &fakeEditor{edit: func(editPath string) error {
			if test.editorErr != nil {
				return test.editorErr
			}
			return os.WriteFile(editPath, []byte(test.content), 0o600)
		}}
		run := runWithDeps(t, commandDeps{editor: editor}, "", "--config", path, "config", "edit")
		if (run.err != nil) != test.wantError {
			t.Fatalf("%s: %+v", test.name, run)
		}
		if !test.wantError {
			document, err := config.Load(path, false)
			if err != nil || document.MergeMethod == nil || *document.MergeMethod != "squash" {
				t.Fatalf("%+v %v", document, err)
			}
		} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid edit saved: %v", err)
		}
	}
}

func TestConfigScopeAndReset(t *testing.T) {
	t.Setenv("DEPFLOW_MERGE_METHOD", "merge")
	if err := os.Unsetenv("DEPFLOW_MERGE_METHOD"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_HOST", "git.example.com")
	path := filepath.Join(t.TempDir(), "config.yaml")
	deps := commandDeps{resolver: &fakeRepoResolver{repo: "git.example.com/acme/tool"}}
	for _, args := range [][]string{
		{"config", "set", "merge-method", "squash"},
		{"config", "set", "merge-method", "rebase", "--repo", "acme/tool"},
		{"config", "reset", "merge-method", "--local"},
	} {
		run := runWithDeps(t, deps, "", append([]string{"--config", path}, args...)...)
		if run.err != nil {
			t.Fatal(run.err)
		}
	}
	run := runWithDeps(t, deps, "", "--config", path, "config", "get", "merge-method", "--local")
	if run.err != nil || strings.TrimSpace(run.stdout) != "squash" {
		t.Fatalf("%+v", run)
	}
	document, err := config.Load(path, false)
	if err != nil || len(document.Repositories) != 0 {
		t.Fatalf("%+v, %v", document, err)
	}
}

func TestConfigEditRepairsAndDetectsConcurrentChanges(t *testing.T) {
	t.Parallel()
	for _, concurrent := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("broken: ["), 0o600); err != nil {
			t.Fatal(err)
		}
		editor := &fakeEditor{edit: func(editPath string) error {
			if concurrent {
				if err := os.WriteFile(path, []byte("merge-method: rebase\n"), 0o600); err != nil {
					return err
				}
			}
			return os.WriteFile(editPath, []byte("merge-method: squash\n"), 0o600)
		}}
		run := runWithDeps(t, commandDeps{editor: editor}, "", "--config", path, "config", "edit")
		if (run.err != nil) != concurrent {
			t.Fatalf("concurrent=%v: %+v", concurrent, run)
		}
		document, err := config.Load(path, false)
		want := "squash"
		if concurrent {
			want = "rebase"
		}
		if err != nil || document.MergeMethod == nil || *document.MergeMethod != want {
			t.Fatalf("%+v, %v", document, err)
		}
	}
}
