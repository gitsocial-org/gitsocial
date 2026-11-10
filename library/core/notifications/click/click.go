// click.go - The Unix socket of a running TUI that the notifier writes the ref of a clicked message to.
package click

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	sendTimeout  = time.Second
	readTimeout  = time.Second
	maxLineBytes = 4096
	maxPathBytes = 103
	socketSuffix = ".sock"
)

// SocketPath returns the socket of the process pid that started at start, in the click directory of cacheDir.
func SocketPath(cacheDir string, pid int, start time.Time) string {
	return filepath.Join(cacheDir, "click", strconv.Itoa(pid)+"-"+strconv.FormatInt(start.Unix(), 10)+socketSuffix)
}

// Listen removes the sockets of dead processes next to path, listens on path, and gives each line it reads to handle; stop closes the listener and removes path.
func Listen(path string, handle func(line string)) (stop func(), err error) {
	if len(path) > maxPathBytes {
		return nil, fmt.Errorf("click socket path is over %d bytes: %s", maxPathBytes, path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create click directory: %w", err)
	}
	removeDeadSockets(dir)
	_ = os.Remove(path) // a socket left by an earlier process with this pid and start time
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on click socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("chmod click socket: %w", err)
	}
	go serve(listener, handle)
	return func() {
		listener.Close()
		_ = os.Remove(path) // the listener can have removed it already
	}, nil
}

// serve accepts connections until the listener closes and reads one line from each.
func serve(listener net.Listener, handle func(line string)) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if line, err := readLine(conn); err == nil {
			handle(line)
		}
		conn.Close()
	}
}

// readLine reads one line of at most maxLineBytes before the read deadline.
func readLine(conn net.Conn) (string, error) {
	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return "", fmt.Errorf("set read deadline: %w", err)
	}
	line, err := bufio.NewReader(io.LimitReader(conn, maxLineBytes)).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read click line: %w", err)
	}
	return strings.TrimSuffix(line, "\n"), nil
}

// removeDeadSockets removes each socket in dir whose pid names no running process.
func removeDeadSockets(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		pidText, _, found := strings.Cut(strings.TrimSuffix(name, socketSuffix), "-")
		pid, err := strconv.Atoi(pidText)
		if !found || !strings.HasSuffix(name, socketSuffix) || err != nil || processAlive(pid) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name)) // another process can remove it first
	}
}

// processAlive reports whether pid names a running process; a process of another user counts as running.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Send writes one line, open and the ref, to the socket at path, and gives up after one second.
func Send(path, ref string) error {
	conn, err := net.DialTimeout("unix", path, sendTimeout)
	if err != nil {
		return fmt.Errorf("dial click socket: %w", err)
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(sendTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := io.WriteString(conn, "open "+ref+"\n"); err != nil {
		return fmt.Errorf("write click line: %w", err)
	}
	return nil
}
