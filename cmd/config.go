// Configuration commands manage user-owned global and repository preferences.
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JakeTRogers/depflow/internal/config"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func newConfigCommand(deps commandDeps, opts *commandOptions) *cobra.Command {
	var local, global bool
	parent := &cobra.Command{Use: "config", Short: "Manage global and repository preferences"}
	parent.PersistentFlags().BoolVar(&local, "local", false, "select the repository inferred by gh (settings stay in the user config)")
	parent.PersistentFlags().BoolVar(&global, "global", false, "select global preferences (the default)")
	scope := func(cmd *cobra.Command) (string, error) {
		if global && (local || cmd.Flags().Changed("repo")) || local && cmd.Flags().Changed("repo") {
			return "", errors.New("choose only one of --global, --local, or --repo")
		}
		if !local && !cmd.Flags().Changed("repo") {
			return "", nil
		}
		repo := opts.repo
		if local {
			resolved, err := resolveRepo(cmd.Context(), deps, "")
			if err != nil {
				return "", err
			}
			repo = resolved
		} else if strings.Count(repo, "/") == 1 && os.Getenv("GH_HOST") != "" {
			repo = os.Getenv("GH_HOST") + "/" + repo
		}
		return config.CanonicalRepo(repo)
	}

	var format string
	var showOrigin bool
	show := &cobra.Command{
		Use: "show", Short: "Display effective preferences", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported output format %q (want text or json)", format)
			}
			repo, err := scope(cmd)
			if err != nil {
				return err
			}
			document, path, err := loadPreferences(opts)
			if err != nil {
				return err
			}
			value, err := document.Resolve(repo, "", false)
			if err != nil {
				return err
			}
			var output any = map[string]string{config.MergeMethod: value.Value}
			if showOrigin {
				output = map[string]any{"path": path, "scope": scopeName(repo), config.MergeMethod: value}
			}
			var data []byte
			if format == "json" {
				data, err = json.MarshalIndent(output, "", "  ")
			} else {
				data, err = yaml.Marshal(output)
			}
			if err != nil {
				return fmt.Errorf("encoding config output: %w", err)
			}
			return printLine(cmd.OutOrStdout(), sanitize(strings.TrimSpace(string(data))))
		},
	}
	show.Flags().StringVar(&format, "format", "text", "output format: text (YAML), json")
	show.Flags().BoolVar(&showOrigin, "show-origin", false, "include preference sources and config path")
	if err := show.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
	get := &cobra.Command{
		Use: "get <key>", Short: "Get an effective preference", Args: cobra.ExactArgs(1), ValidArgsFunction: configCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != config.MergeMethod {
				return fmt.Errorf("unknown config key %q", args[0])
			}
			repo, err := scope(cmd)
			if err != nil {
				return err
			}
			document, _, err := loadPreferences(opts)
			if err != nil {
				return err
			}
			value, err := document.Resolve(repo, "", false)
			if err != nil {
				return err
			}
			return printLine(cmd.OutOrStdout(), value.Value)
		},
	}
	set := &cobra.Command{
		Use: "set <key> <value>", Short: "Save a preference in the selected scope", Args: cobra.ExactArgs(2), ValidArgsFunction: configCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := scope(cmd)
			if err != nil {
				return err
			}
			path, err := config.Path(opts.configPath)
			if err != nil {
				return err
			}
			if err := config.Update(path, func(document *config.Document) error { return document.Set(repo, args[0], args[1]) }); err != nil {
				return err
			}
			return printLine(cmd.OutOrStdout(), fmt.Sprintf("Set %s = %s (%s)", args[0], args[1], scopeName(repo)))
		},
	}
	var yes bool
	reset := &cobra.Command{
		Use: "reset [key]", Short: "Remove saved overrides from the selected scope", Args: cobra.MaximumNArgs(1), ValidArgsFunction: configCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := scope(cmd)
			if err != nil {
				return err
			}
			key := ""
			if len(args) == 1 {
				key = args[0]
			} else if !yes {
				return errors.New("resetting all preferences in a scope requires --yes; other scopes are preserved")
			}
			path, err := config.Path(opts.configPath)
			if err != nil {
				return err
			}
			if err := config.Update(path, func(document *config.Document) error { return document.Reset(repo, key) }); err != nil {
				return err
			}
			return printLine(cmd.OutOrStdout(), "Removed saved overrides ("+scopeName(repo)+"); inherited values now apply")
		},
	}
	reset.Flags().BoolVar(&yes, "yes", false, "confirm resetting all preferences in the selected scope")
	pathCommand := &cobra.Command{
		Use: "path", Short: "Print the preferences file path", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path(opts.configPath)
			if err != nil {
				return err
			}
			return printLine(cmd.OutOrStdout(), sanitize(path))
		},
	}
	edit := &cobra.Command{
		Use: "edit", Short: "Edit the complete preferences file in $VISUAL/$EDITOR", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if local || global || cmd.Flags().Changed("repo") {
				return errors.New("config edit edits the whole file; omit --local, --global, and --repo")
			}
			return editPreferences(cmd, deps, opts)
		},
	}
	parent.AddCommand(show, get, set, reset, pathCommand, edit)
	return parent
}

func loadPreferences(opts *commandOptions) (config.Document, string, error) {
	path, err := config.Path(opts.configPath)
	if err != nil {
		return config.Document{}, "", err
	}
	document, err := config.Load(path, opts.configPath == "")
	return document, path, err
}

func scopeName(repo string) string {
	if repo == "" {
		return "global"
	}
	return repo
}

func configCompletions(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return []string{cobra.CompletionWithDesc(config.MergeMethod, "preferred PR merge method")}, cobra.ShellCompDirectiveNoFileComp
	}
	if cmd.Name() == "set" && len(args) == 1 && args[0] == config.MergeMethod {
		return mergeMethodCandidates([]string{"merge", "squash", "rebase"}), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func mergeMethodCandidates(methods []string) []string {
	var candidates []string
	for _, entry := range []struct{ name, description string }{
		{"merge", "Create a merge commit"}, {"squash", "Squash and merge"}, {"rebase", "Rebase and merge"},
	} {
		if slices.Contains(methods, entry.name) {
			candidates = append(candidates, cobra.CompletionWithDesc(entry.name, entry.description))
		}
	}
	return candidates
}

func editPreferences(cmd *cobra.Command, deps commandDeps, opts *commandOptions) error {
	if deps.editor == nil {
		return errors.New("no editor configured")
	}
	path, err := config.Path(opts.configPath)
	if err != nil {
		return err
	}
	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading config: %w", err)
	}
	existed := err == nil
	initial := original
	if !existed {
		initial = []byte("version: 1\n")
	}
	file, err := os.CreateTemp("", "depflow-config-*.yaml")
	if err != nil {
		return fmt.Errorf("creating config edit file: %w", err)
	}
	defer func() {
		_ = os.Remove(file.Name())
	}()
	if _, err := file.Write(initial); err != nil {
		_ = file.Close()
		return fmt.Errorf("preparing config edit: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := deps.editor.Edit(cmd.Context(), file.Name()); err != nil {
		return err
	}
	edited, err := os.ReadFile(file.Name())
	if err != nil {
		return fmt.Errorf("reading edited config: %w", err)
	}
	document, err := config.Parse(edited)
	if err != nil {
		return fmt.Errorf("edited config is invalid; original unchanged: %w", err)
	}
	if err := config.SaveEdited(path, original, existed, document); err != nil {
		return err
	}
	return printLine(cmd.OutOrStdout(), "Updated "+sanitize(filepath.Clean(path)))
}
