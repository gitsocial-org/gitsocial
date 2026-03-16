// config.go - CLI commands for managing extension configuration
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
)

const coreExt = "core"

// NewExtConfigCmd creates a config command with get/set/list subcommands for the given extension.
func NewExtConfigCmd(ext string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: fmt.Sprintf("Manage %s extension configuration", ext),
	}
	cmd.AddCommand(
		newExtConfigGetCmd(ext),
		newExtConfigSetCmd(ext),
		newExtConfigListCmd(ext),
	)
	return cmd
}

func newExtConfigGetCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			value, ok := gitmsg.GetExtConfigValue(cfg.WorkDir, ext, key)
			if !ok {
				PrintError(cmd, fmt.Sprintf("key not found: %s", key))
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"key": key, "value": value})
			} else {
				fmt.Println(value)
			}
		},
	}
}

func newExtConfigSetCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			value := args[1]
			if err := gitmsg.SetExtConfigValue(cfg.WorkDir, ext, key, value); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"key": key, "value": value})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("%s = %s", key, value))
			}
		},
	}
}

func newExtConfigListCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all config values",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			items := gitmsg.ListExtConfig(cfg.WorkDir, ext)
			if cfg.JSONOutput {
				PrintJSON(items)
			} else {
				if len(items) == 0 {
					fmt.Println("No config set")
					return
				}
				for _, item := range items {
					fmt.Printf("%s = %s\n", item.Key, item.Value)
				}
			}
		},
	}
}
