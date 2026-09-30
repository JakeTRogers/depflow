package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreferencesRoundTrip(t *testing.T) {
	t.Setenv("DEPFLOW_MERGE_METHOD", "rebase")
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := Update(path, func(document *Document) error {
		if err := document.Set("", MergeMethod, "squash"); err != nil {
			return err
		}
		return document.Set("git.example.com/Acme/Tool", MergeMethod, "merge")
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if *document.MergeMethod != "squash" || *document.Repositories["git.example.com/acme/tool"].MergeMethod != "merge" {
		t.Fatalf("wrong saved settings: %+v", document)
	}
	value, err := document.Resolve("git.example.com/acme/tool", "", false)
	if err != nil || value.Value != "rebase" || value.Source != "DEPFLOW_MERGE_METHOD" {
		t.Fatalf("environment resolution = %+v, %v", value, err)
	}
	value, err = document.Resolve("git.example.com/acme/tool", "squash", true)
	if err != nil || value.Value != "squash" || value.Source != "--merge-method" {
		t.Fatalf("flag resolution = %+v, %v", value, err)
	}
	if err := os.Unsetenv("DEPFLOW_MERGE_METHOD"); err != nil {
		t.Fatal(err)
	}
	value, err = document.Resolve("git.example.com/acme/tool", "", false)
	if err != nil || value.Value != "merge" || !strings.Contains(value.Source, "repository") {
		t.Fatalf("repo resolution = %+v, %v", value, err)
	}
	if err := Update(path, func(document *Document) error { return document.Reset("git.example.com/acme/tool", MergeMethod) }); err != nil {
		t.Fatal(err)
	}
	document, err = Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	value, err = document.Resolve("git.example.com/acme/tool", "", false)
	if err != nil || value.Value != "squash" || value.Source != "global preference" {
		t.Fatalf("inheritance = %+v, %v", value, err)
	}
}

func TestREADMEConfiguration(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(readme), "### config\n")
	if !ok {
		t.Fatal("README configuration section not found")
	}
	_, block, ok := strings.Cut(section, "\n```yaml\n")
	if !ok {
		t.Fatal("README configuration YAML block not found")
	}
	example, _, ok := strings.Cut(block, "\n```")
	if !ok {
		t.Fatal("README configuration YAML block is not closed")
	}
	document, err := Parse([]byte(example))
	if err != nil {
		t.Fatalf("README configuration is invalid: %v", err)
	}
	if document.MergeMethod == nil || len(document.Repositories) == 0 {
		t.Fatal("README configuration must demonstrate global and repository preferences")
	}
}

func TestInvalidDocuments(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		"version: 2", "merge-method: invalid", "merge-method: 42", "typo: squash", "[broken",
		"repositories:\n  owner/repo:\n    merge-method: squash", "repositories:\n  github.com/owner/repo:\n    merge-method: wrong",
		"repositories:\n  github.com/owner/repo:\n    typo: squash",
	} {
		t.Run(data, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(data)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUpdatePreservesOnFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Update(path, func(document *Document) error { return document.Set("", MergeMethod, "merge") }); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = Update(path, func(document *Document) error { return errors.New("cancelled") })
	if err == nil {
		t.Fatal("expected error")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("original changed: %s, %v", after, err)
	}
	if err := os.WriteFile(path+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, func(document *Document) error { return nil }); err == nil {
		t.Fatal("expected lock conflict")
	}
}

func TestConfigValidationAndDefaults(t *testing.T) {
	t.Setenv("DEPFLOW_MERGE_METHOD", "")
	if _, err := (Document{Version: 1}).Resolve("", "", false); err == nil {
		t.Fatal("empty environment must be invalid")
	}
	if err := os.Unsetenv("DEPFLOW_MERGE_METHOD"); err != nil {
		t.Fatal(err)
	}
	document, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := document.Resolve("", "", false)
	if err != nil || value.Value != "merge" || value.Source != "built-in default" {
		t.Fatalf("%+v, %v", value, err)
	}
	if err := document.Set("", "bad", "merge"); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := document.Set("bad", "merge-method", "merge"); err == nil {
		t.Fatal("bad repo accepted")
	}
	if err := document.Reset("", "bad"); err == nil {
		t.Fatal("unknown reset key accepted")
	}
	if err := document.Reset("", ""); err != nil {
		t.Fatal(err)
	}
	path, err := Path(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil || !filepath.IsAbs(path) {
		t.Fatalf("%s %v", path, err)
	}
	if _, err := Path(""); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, false); err == nil {
		t.Fatal("explicit missing path accepted")
	}
	if _, err := Load(path, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bad: key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, false); err == nil {
		t.Fatal("bad file accepted")
	}
	for _, repo := range []string{"bad", "https://github.com/acme/tool", "github.com/acme/..", "github.com/acme/."} {
		if _, err := CanonicalRepo(repo); err == nil {
			t.Fatalf("bad repo accepted: %s", repo)
		}
	}
}
