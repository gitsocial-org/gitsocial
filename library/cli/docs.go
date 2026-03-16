// docs.go - Documentation generation commands
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/tui/tuikeydoc"
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
		Run: func(cmd *cobra.Command, args []string) {
			docs := tuikeydoc.CollectAll()
			fmt.Print(tuikeydoc.Generate(docs))
		},
	}
}
