// settings.go - CLI commands for managing user settings
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/settings"
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
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			key := args[0]

			path, err := settings.DefaultPath()
			if err != nil {
				PrintError(cmd, "failed to get settings path: "+err.Error())
				os.Exit(ExitError)
			}

			s, err := settings.Load(path)
			if err != nil {
				PrintError(cmd, "failed to load settings: "+err.Error())
				os.Exit(ExitError)
			}

			value, ok := settings.Get(s, key)
			if !ok {
				PrintError(cmd, "unknown key: "+key)
				os.Exit(ExitInvalidArgs)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{"key": key, "value": value})
			} else {
				fmt.Println(value)
			}
		},
	}
}

// newSettingsSetCmd creates the command to set a settings key-value pair.
func newSettingsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a settings value",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			key := args[0]
			value := args[1]

			path, err := settings.DefaultPath()
			if err != nil {
				PrintError(cmd, "failed to get settings path: "+err.Error())
				os.Exit(ExitError)
			}

			s, err := settings.Load(path)
			if err != nil {
				PrintError(cmd, "failed to load settings: "+err.Error())
				os.Exit(ExitError)
			}

			if err := settings.Set(s, key, value); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitInvalidArgs)
			}

			if err := settings.Save(path, s); err != nil {
				PrintError(cmd, "failed to save settings: "+err.Error())
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

// newSettingsListCmd creates the command to list all settings values.
func newSettingsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all settings values",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)

			path, err := settings.DefaultPath()
			if err != nil {
				PrintError(cmd, "failed to get settings path: "+err.Error())
				os.Exit(ExitError)
			}

			s, err := settings.Load(path)
			if err != nil {
				PrintError(cmd, "failed to load settings: "+err.Error())
				os.Exit(ExitError)
			}

			items := settings.ListAll(s)

			if cfg.JSONOutput {
				PrintJSON(items)
			} else {
				for _, item := range items {
					if item.Value != "" {
						fmt.Printf("%s = %s\n", item.Key, item.Value)
					} else {
						fmt.Printf("%s = (not set)\n", item.Key)
					}
				}
			}
		},
	}
}
