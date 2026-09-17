// settings.go - CLI commands for managing user settings
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// newSettingsCmd creates the parent command for managing user settings.
func newSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage user settings",
	}

	cmd.AddCommand(
		newSettingsGetCmd(),
		newSettingsSetCmd(),
		newSettingsListCmd(),
	)

	return cmd
}

// newSettingsGetCmd creates the command to retrieve a settings value by key.
func newSettingsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a settings value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			key := args[0]

			path, err := settings.DefaultPath()
			if err != nil {
				PrintError(cmd, "resolve settings path: "+err.Error())
				return exit(ExitError)
			}

			s, err := settings.Load(path)
			if err != nil {
				PrintError(cmd, "load settings: "+err.Error())
				return exit(ExitError)
			}

			value, ok := settings.Get(s, key)
			if !ok {
				PrintError(cmd, "unknown key "+key+": run gitsocial settings list")
				return exit(ExitInvalidArgs)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"key": key, "value": value})
			} else {
				fmt.Println(value)
			}
			return nil
		},
	}
}

// newSettingsSetCmd creates the command to set a settings key-value pair.
// Writes are dispatched to the personal-config ref; the bare repo is auto-
// initialized at PersonalRepoPath() on first write.
func newSettingsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a settings value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			key := args[0]
			value := args[1]

			if err := settings.NewManager().Write(key, value); err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitInvalidArgs)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"key": key, "value": value})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("%s = %s", key, value))
			}
			return nil
		},
	}
}

// newSettingsListCmd creates the command to list all settings values.
func newSettingsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all settings values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)

			path, err := settings.DefaultPath()
			if err != nil {
				PrintError(cmd, "resolve settings path: "+err.Error())
				return exit(ExitError)
			}

			s, err := settings.Load(path)
			if err != nil {
				PrintError(cmd, "load settings: "+err.Error())
				return exit(ExitError)
			}

			items := settings.ListAll(s)

			if cfg.JSONOutput {
				return PrintJSON(cmd, items)
			} else {
				for _, item := range items {
					if item.Value != "" {
						fmt.Printf("%s = %s\n", item.Key, item.Value)
					} else {
						fmt.Printf("%s = (not set)\n", item.Key)
					}
				}
			}
			return nil
		},
	}
}
