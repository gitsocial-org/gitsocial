// docs.go - Documentation generation commands
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/tui/tuikeydoc"
)

func newDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate documentation",
	}
	cmd.AddCommand(newDocsKeybindingsCmd())
	return cmd
}

func newDocsKeybindingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keybindings",
		Short: "Generate keybinding documentation",
		RunE: func(cmd *cobra.Command, args []string) error {
			docs := tuikeydoc.CollectAll()
			if cfg := GetConfig(cmd); cfg != nil && cfg.JSONOutput {
				return PrintJSON(cmd, docs)
			}
			fmt.Print(tuikeydoc.Generate(docs))
			return nil
		},
	}
}
