package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var version = "1.1.0"

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "depflow %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH); err != nil {
				return fmt.Errorf("writing version output: %w", err)
			}
			return nil
		},
	}
}
