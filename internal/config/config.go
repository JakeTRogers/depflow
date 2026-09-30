// Package config stores user preferences separately from effective run settings.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// MergeMethod is the supported preference key.
const MergeMethod = "merge-method"

// Settings contains explicitly saved preferences; nil values inherit.
type Settings struct {
	MergeMethod *string `mapstructure:"merge-method" yaml:"merge-method,omitempty"`
}

// Document is the versioned, user-owned configuration file.
type Document struct {
	Version      int                 `mapstructure:"version" yaml:"version"`
	MergeMethod  *string             `mapstructure:"merge-method" yaml:"merge-method,omitempty"`
	Repositories map[string]Settings `mapstructure:"repositories" yaml:"repositories,omitempty"`
}

// Value includes the effective preference and its origin.
type Value struct {
	Value  string `json:"value" yaml:"value"`
	Source string `json:"source" yaml:"source"`
}

// Path returns the explicit path or the platform's user configuration path.
func Path(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving config directory: %w", err)
	}
	return filepath.Join(dir, "depflow", "config.yaml"), nil
}

// ValidateMethod validates the spelling shared by preferences and CLI flags.
func ValidateMethod(value string) error {
	switch value {
	case "merge", "squash", "rebase":
		return nil
	default:
		return fmt.Errorf("invalid merge method %q (want merge, squash, or rebase)", value)
	}
}

var repoPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*/[a-zA-Z0-9][a-zA-Z0-9-]*/[a-zA-Z0-9_.-]+$`)

// CanonicalRepo normalizes a repository to a case-insensitive HOST/OWNER/REPO key.
func CanonicalRepo(repo string) (string, error) {
	if strings.Count(repo, "/") == 1 {
		repo = "github.com/" + repo
	}
	if !repoPattern.MatchString(repo) {
		return "", fmt.Errorf("invalid repository %q (want [HOST/]OWNER/REPO)", repo)
	}
	parts := strings.Split(repo, "/")
	if parts[2] == "." || parts[2] == ".." {
		return "", fmt.Errorf("invalid repository %q", repo)
	}
	return strings.ToLower(repo), nil
}

// Parse decodes only supported settings, keeping hostnames as literal map keys.
func Parse(data []byte) (Document, error) {
	reader := viper.NewWithOptions(viper.KeyDelimiter("::"))
	reader.SetConfigType("yaml")
	reader.SetDefault("version", 1)
	if err := reader.ReadConfig(bytes.NewReader(data)); err != nil {
		return Document{}, fmt.Errorf("parsing configuration: %w", err)
	}
	var document Document
	if err := reader.UnmarshalExact(&document, func(cfg *mapstructure.DecoderConfig) {
		cfg.WeaklyTypedInput = false
	}); err != nil {
		return Document{}, fmt.Errorf("decoding configuration: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Load reads a configuration file, allowing missing files only when requested.
func Load(path string, allowMissing bool) (Document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && allowMissing {
		return Document{Version: 1}, nil
	}
	if err != nil {
		return Document{}, fmt.Errorf("reading config %s: %w", path, err)
	}
	document, err := Parse(data)
	if err != nil {
		return Document{}, fmt.Errorf("config %s: %w", path, err)
	}
	return document, nil
}

// Validate rejects unsupported schemas, repository identities, and methods.
func (document Document) Validate() error {
	if document.Version != 1 {
		return fmt.Errorf("unsupported config version %d (want 1)", document.Version)
	}
	if document.MergeMethod != nil {
		if err := ValidateMethod(*document.MergeMethod); err != nil {
			return fmt.Errorf("global preference: %w", err)
		}
	}
	for repo, settings := range document.Repositories {
		canonical, err := CanonicalRepo(repo)
		if err != nil || canonical != repo {
			return fmt.Errorf("repository key %q must be a lowercase HOST/OWNER/REPO", repo)
		}
		if settings.MergeMethod != nil {
			if err := ValidateMethod(*settings.MergeMethod); err != nil {
				return fmt.Errorf("repository %s: %w", repo, err)
			}
		}
	}
	return nil
}

// Resolve applies environment and run overrides without changing saved settings.
func (document Document) Resolve(repo, override string, overrideSet bool) (Value, error) {
	values := viper.New()
	values.SetDefault(MergeMethod, "merge")
	source := "built-in default"
	if document.MergeMethod != nil {
		values.SetDefault(MergeMethod, *document.MergeMethod)
		source = "global preference"
	}
	if settings, ok := document.Repositories[repo]; ok && settings.MergeMethod != nil {
		values.SetDefault(MergeMethod, *settings.MergeMethod)
		source = "repository preference (" + repo + ")"
	}
	if err := values.BindEnv(MergeMethod, "DEPFLOW_MERGE_METHOD"); err != nil {
		return Value{}, fmt.Errorf("binding merge method environment: %w", err)
	}
	values.AllowEmptyEnv(true)
	if _, ok := os.LookupEnv("DEPFLOW_MERGE_METHOD"); ok {
		source = "DEPFLOW_MERGE_METHOD"
	}
	if overrideSet {
		values.Set(MergeMethod, override)
		source = "--merge-method"
	}
	value := values.GetString(MergeMethod)
	if err := ValidateMethod(value); err != nil {
		return Value{}, fmt.Errorf("%s: %w", source, err)
	}
	return Value{Value: value, Source: source}, nil
}

// Set changes a saved preference; an empty repo selects global settings.
func (document *Document) Set(repo, key, value string) error {
	if key != MergeMethod {
		return fmt.Errorf("unknown config key %q", key)
	}
	if err := ValidateMethod(value); err != nil {
		return err
	}
	if repo == "" {
		document.MergeMethod = &value
		return nil
	}
	canonical, err := CanonicalRepo(repo)
	if err != nil {
		return err
	}
	if document.Repositories == nil {
		document.Repositories = make(map[string]Settings)
	}
	document.Repositories[canonical] = Settings{MergeMethod: &value}
	return nil
}

// Reset removes saved overrides from one scope, restoring inheritance.
func (document *Document) Reset(repo, key string) error {
	if key != "" && key != MergeMethod {
		return fmt.Errorf("unknown config key %q", key)
	}
	if repo == "" {
		document.MergeMethod = nil
	} else {
		delete(document.Repositories, repo)
	}
	return nil
}

// Update serializes cooperating writers and replaces only a validated document.
func Update(path string, change func(*Document) error) error {
	return writeLocked(path, func() (Document, error) {
		document, err := Load(path, true)
		if err != nil {
			return Document{}, err
		}
		if err := change(&document); err != nil {
			return Document{}, err
		}
		return document, nil
	})
}

// SaveEdited replaces a possibly malformed file only if it has not changed since editing began.
func SaveEdited(path string, original []byte, existed bool, document Document) error {
	return writeLocked(path, func() (Document, error) {
		current, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Document{}, fmt.Errorf("reading current configuration: %w", err)
		}
		if existed != (err == nil) || !bytes.Equal(current, original) {
			return Document{}, errors.New("configuration changed while editing; refusing to overwrite it")
		}
		return document, nil
	})
}

func writeLocked(path string, readDocument func() (Document, error)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("locking config (another writer or stale lock at %s): %w", lockPath, err)
	}
	defer func() {
		_ = os.Remove(lockPath)
	}()
	if err := lock.Close(); err != nil {
		return fmt.Errorf("closing config lock: %w", err)
	}
	document, err := readDocument()
	if err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("encoding configuration: %w", err)
	}
	return replace(path, data)
}

func replace(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".depflow-config-*")
	if err != nil {
		return fmt.Errorf("creating config temporary file: %w", err)
	}
	defer func() {
		_ = os.Remove(file.Name())
	}()
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("writing configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing configuration: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replacing configuration: %w", err)
	}
	return nil
}
