// main.go - locals3, a disk-backed local S3 server for development and the site-test fixture builder
//
// The handler lives in core/objstore/localbucket, shared with `gitsocial
// serve`; this main adds only the flags and the listener, so the build path
// and the site-test fixtures do not change.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/gitsocial-org/gitsocial/library/core/objstore/localbucket"
)

// main binds the listener (ephemeral by default) and serves until killed.
func main() {
	// 9000 is the S3-ecosystem convention, and gives a stable port for a persisted loopback remote URL.
	addr := flag.String("addr", "127.0.0.1:9000", "listen address")
	root := flag.String("root", "", "bucket root directory")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "missing -root")
		os.Exit(1)
	}
	if err := os.MkdirAll(*root, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("listening %s root=%s\n", ln.Addr().String(), *root)
	if err := http.Serve(ln, localbucket.Handler(*root)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
