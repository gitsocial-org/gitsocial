// registry.go - Extension command registration for CLI scalability
package main

import "github.com/spf13/cobra"

// ExtensionRegistration holds metadata and registration function for an extension.
type ExtensionRegistration struct {
	Use      string
	Short    string
	Register func(cmd *cobra.Command)
}

var extensionRegistry []ExtensionRegistration

// RegisterExtension registers an extension's commands to be added at startup.
// Called from init() in each extension's CLI file.
func RegisterExtension(ext ExtensionRegistration) {
	extensionRegistry = append(extensionRegistry, ext)
}

// RegisterAllExtensions adds all registered extension commands to the root command.
func RegisterAllExtensions(rootCmd *cobra.Command) {
	for _, ext := range extensionRegistry {
		extCmd := &cobra.Command{
			Use:   ext.Use,
			Short: ext.Short,
		}
		ext.Register(extCmd)
		rootCmd.AddCommand(extCmd)
	}
}
