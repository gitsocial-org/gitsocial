// main.go - CLI entry point, command registration, and TUI launcher
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/tui"
)

var version = "dev"

// startProfiling wires CPU/memory/trace profiling controlled by GITSOCIAL_PPROF.
// Modes: "cpu" (default file /tmp/gitsocial-cpu.pprof), "mem" (heap snapshot at
// exit -> /tmp/gitsocial-mem.pprof), "trace" (execution trace -> /tmp/gitsocial.trace).
// The returned stop function should be deferred so output flushes on exit.
func startProfiling() func() {
	mode := os.Getenv("GITSOCIAL_PPROF")
	if mode == "" {
		return func() {}
	}
	switch mode {
	case "cpu":
		f, err := os.Create("/tmp/gitsocial-cpu.pprof")
		if err != nil {
			fmt.Fprintf(os.Stderr, "pprof: cannot create cpu profile: %v\n", err)
			return func() {}
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "pprof: cannot start cpu profile: %v\n", err)
			_ = f.Close()
			return func() {}
		}
		fmt.Fprintln(os.Stderr, "pprof: capturing CPU profile to /tmp/gitsocial-cpu.pprof")
		return func() {
			pprof.StopCPUProfile()
			_ = f.Close()
			fmt.Fprintln(os.Stderr, "pprof: wrote /tmp/gitsocial-cpu.pprof — analyze with: go tool pprof /tmp/gitsocial-cpu.pprof")
		}
	case "mem":
		fmt.Fprintln(os.Stderr, "pprof: will write heap snapshot to /tmp/gitsocial-mem.pprof on exit")
		return func() {
			f, err := os.Create("/tmp/gitsocial-mem.pprof")
			if err != nil {
				fmt.Fprintf(os.Stderr, "pprof: cannot create mem profile: %v\n", err)
				return
			}
			defer f.Close()
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "pprof: cannot write mem profile: %v\n", err)
				return
			}
			fmt.Fprintln(os.Stderr, "pprof: wrote /tmp/gitsocial-mem.pprof — analyze with: go tool pprof /tmp/gitsocial-mem.pprof")
		}
	case "trace":
		f, err := os.Create("/tmp/gitsocial.trace")
		if err != nil {
			fmt.Fprintf(os.Stderr, "pprof: cannot create trace: %v\n", err)
			return func() {}
		}
		if err := trace.Start(f); err != nil {
			fmt.Fprintf(os.Stderr, "pprof: cannot start trace: %v\n", err)
			_ = f.Close()
			return func() {}
		}
		fmt.Fprintln(os.Stderr, "pprof: capturing execution trace to /tmp/gitsocial.trace")
		return func() {
			trace.Stop()
			_ = f.Close()
			fmt.Fprintln(os.Stderr, "pprof: wrote /tmp/gitsocial.trace — analyze with: go tool trace /tmp/gitsocial.trace")
		}
	default:
		fmt.Fprintf(os.Stderr, "pprof: unknown GITSOCIAL_PPROF=%q (use cpu|mem|trace)\n", mode)
		return func() {}
	}
}

// main is the CLI entry point that registers commands and executes the root command.
func main() {
	stop := startProfiling()
	defer stop()

	rootCmd := newRootCmd()

	// Core commands
	rootCmd.AddCommand(
		newStatusCmd(),
		newFetchCmd(),
		newPushCmd(),
		NewExtConfigCmd(coreExt),
		newSettingsCmd(),
		newLogCmd(),
		newSearchCmd(),
		newShowCmd(),
		newRelatedCmd(),
		newExploreCmd(),
		newHistoryCmd(),
		newNotificationsCmd(),
		newTUICmd(),
		newDocsCmd(),
		newRPCCmd(),
		newImportCmd(),
		newForkCmd(),
		newIDCmd(),
	)

	// Extension commands (auto-registered via init())
	RegisterAllExtensions(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitError)
	}
}

// newTUICmd creates the command for launching the interactive TUI.
func newTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch interactive TUI for browsing posts",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := tui.Run(cfg.WorkDir, cfg.CacheDir); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
		},
	}

	return cmd
}
