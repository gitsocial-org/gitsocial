// serve.go - CLI command that serves the site of the workspace from a loopback bucket
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/objstore/localbucket"
)

const (
	serveRemote      = "gitsocial-serve"
	serveBucket      = "gitsocial"
	serveDefaultAddr = "127.0.0.1:4747"
	serveInterval    = 2 * time.Second
)

// newServeCmd creates the serve command.
func newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the site of the workspace on loopback",
		Long: `Show the site of the workspace in a browser. serve starts a loopback
bucket in the process, pushes the workspace to it and serves the site
app from the bucket. A change to a branch, a tag or a state ref pushes
again. The site has no HTML pages. Ctrl-C stops it.

The bucket is at <cache-dir>/serve/<hash>/ and stays between runs, so
the next start pushes the difference. The remote gitsocial-serve is in
the environment of the serve process only; serve writes no git config,
and it deletes the tracking refs of the remote on exit. A push to the
bucket runs no git hook and is a forced push, so a rewritten branch
replaces the branch of the bucket.

Examples:
  gitsocial serve                     # serving http://127.0.0.1:4747/gitsocial/<repo>/
  gitsocial serve --addr 127.0.0.1:8080`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if err := runServe(ctx, GetConfig(cmd), addr, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", serveDefaultAddr, "Listen address")
	return cmd
}

// runServe serves the bucket on addr, pushes the workspace to it and again on each ref change until ctx ends, then drops the tracking refs.
func runServe(ctx context.Context, cfg *Config, addr string, out, errOut io.Writer) error {
	root, err := git.GetRootDir(cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("resolve the workspace root: %w", err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: choose another port with --addr: %w", addr, err)
	}
	bucketRoot := filepath.Join(cfg.CacheDir, "serve", fetch.URLHash(root))
	if err := os.MkdirAll(bucketRoot, 0o755); err != nil {
		ln.Close()
		return fmt.Errorf("create %s: %w", bucketRoot, err)
	}
	server := &http.Server{Handler: localbucket.Handler(bucketRoot), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(ln) }() // Serve ends with ErrServerClosed on Close
	defer server.Close()
	host := serveHost(ln.Addr())
	path := serveBucket + "/" + filepath.Base(root)
	if err := applyServeEnv(host, "s3://"+host+"/"+path); err != nil {
		return err
	}
	push := func() { servePush(ctx, root, errOut) }
	last := refsSnapshot(root)
	push()
	fmt.Fprint(out, serveBanner("http://"+host+"/"+path+"/", out == io.Writer(os.Stdout) && isatty.IsTerminal(os.Stdout.Fd()), os.Getenv("NO_COLOR") == ""))
	watchRefs(ctx, serveInterval, last, func() string { return refsSnapshot(root) }, push)
	client.DeleteTrackingRefs(root, serveRemote)
	return nil
}

// serveBanner returns the line that tells the site URL: a block on a terminal, with colors when color is set, and the plain serving line for a pipe.
func serveBanner(url string, terminal, color bool) string {
	if !terminal {
		return "serving " + url + "\n"
	}
	bold, green, cyan, dim, reset := "\033[1m", "\033[32m", "\033[36m", "\033[2m", "\033[0m"
	if !color {
		bold, green, cyan, dim, reset = "", "", "", "", ""
	}
	return "\n  " + bold + "gitsocial serve" + reset + "\n\n" +
		"  " + green + "➜" + reset + "  " + bold + "Local:" + reset + "  " + cyan + url + reset + "\n" +
		"  " + green + "➜" + reset + "  " + dim + "Ctrl-C stops the server; a ref change pushes again" + reset + "\n\n"
}

// refsSnapshot returns the names and tips of the branches, tags and state refs, or "" when git cannot list them.
func refsSnapshot(root string) string {
	out, err := git.ExecGit(root, []string{"for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/tags", "refs/gitmsg"})
	if err != nil {
		return ""
	}
	return out.Stdout
}

// watchRefs polls the snapshot every interval and runs push on each change until ctx ends; one push runs at a time, and a change during a push gives one more push after it.
func watchRefs(ctx context.Context, interval time.Duration, last string, snapshot func() string, push func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		current := snapshot()
		if current == last {
			continue
		}
		last = current
		push()
	}
}

// serveHost returns the host and port a client reaches the listener at; an unspecified address answers on loopback.
func serveHost(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || !tcp.IP.IsUnspecified() {
		return addr.String()
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(tcp.Port))
}

// serveConfigPairs returns the git config of the serve remote: its URL, its fetch refspec, the site overrides, the site switch and an empty hooks path.
func serveConfigPairs(remoteURL string) [][2]string {
	return [][2]string{
		{"remote." + serveRemote + ".url", remoteURL},
		{"remote." + serveRemote + ".fetch", "+refs/heads/*:refs/remotes/" + serveRemote + "/*"},
		{"remote." + serveRemote + "." + objstore.SiteOverridePublishKey, "true"},
		{"remote." + serveRemote + "." + objstore.SiteOverridePagesKey, "false"},
		{"gitsocial.pushSite", "true"},
		{"core.hooksPath", os.DevNull},
	}
}

// applyServeEnv puts the serve remote into the process environment after the config entries already there, with placeholder keys when the host has none.
func applyServeEnv(host, remoteURL string) error {
	for _, entry := range git.AppendConfigEnv(os.Environ(), serveConfigPairs(remoteURL)...) {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, "GIT_CONFIG_") {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	if objstore.HasCredentials(host) {
		return nil
	}
	for _, key := range []string{"GITSOCIAL_S3_ACCESS_KEY", "GITSOCIAL_S3_SECRET_KEY"} {
		if err := os.Setenv(key, "serve"); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return nil
}

// serveSitePassCap bounds the pushes that finish a site bootstrap.
const serveSitePassCap = 16

// servePush pushes the workspace to the serve remote until the site is complete and reports a failure; the served build stays, and a stopped serve reports nothing.
func servePush(ctx context.Context, root string, errOut io.Writer) {
	progress, step, finish := objstore.WriterProgress(errOut)
	defer finish()
	onBranch := func(branch string, done, total int) { step(serveRemote+" "+branch, done, total) }
	for pass := 0; pass < serveSitePassCap; pass++ {
		res, err := client.Push(root, serveRemote, client.Options{Force: true}, onBranch, progress)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			fmt.Fprintf(errOut, "gitsocial: push: %v\n", err)
			return
		case res.Site.Err != nil:
			fmt.Fprintf(errOut, "gitsocial: site: %v\n", res.Site.Err)
			return
		case res.Site.Skipped != "":
			fmt.Fprintf(errOut, "gitsocial: site skipped: %s\n", res.Site.Skipped)
			return
		case res.Site.Complete:
			return
		}
	}
}
