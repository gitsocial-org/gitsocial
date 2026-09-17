// rpc.go - JSON-RPC server command for editor integrations
package main

import (
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/rpc"
)

// newRPCCmd creates the command for starting the JSON-RPC stdio server.
func newRPCCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "rpc",
		Short:  "Start JSON-RPC server on stdio",
		Hidden: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Override root PersistentPreRunE — no cache.Open, no workdir resolve.
			// Cache opens during initialize (future milestone).
			initLogging(cmd.ErrOrStderr(), false)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			registry := rpc.NewRegistry()
			server := rpc.NewServer(registry, cmd.InOrStdin(), cmd.OutOrStdout())
			rpc.RegisterCoreMethods(server, version)
			rpc.RegisterSearchMethods(server)
			rpc.RegisterSocialMethods(server)
			rpc.RegisterPMMethods(server)
			rpc.RegisterReviewMethods(server)
			rpc.RegisterReleaseMethods(server)
			return server.Run()
		},
	}
}
