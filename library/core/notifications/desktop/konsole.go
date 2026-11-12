// konsole.go - Selects the tab of a Konsole session and brings its window to the front through the window runner of KWin.
package desktop

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
)

const (
	kwinName          = "org.kde.KWin"
	kwinRunnerPath    = "/WindowsRunner"
	krunnerName       = "org.kde.krunner1"
	konsoleWindowName = "org.kde.konsole.Window"
	konsoleWindows    = 16
)

// captionDash is the separator of a KWin caption between the tab title and the application name.
const captionDash = " \u2014 "

// konsoleSession returns the session number of the Konsole session path "/Sessions/<number>" when service is set.
func konsoleSession(service, sessionPath string) (int32, bool) {
	number, ok := strings.CutPrefix(sessionPath, "/Sessions/")
	if !ok || service == "" || number == "" || strings.Trim(number, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseInt(number, 10, 32)
	if err != nil || n < 1 {
		return 0, false
	}
	return int32(n), true
}

// konsoleActivate returns the exchange that finds the Konsole window of session, finds it in KWin by the title of its current tab, selects the tab of session and runs the window; each failed step stops it with a debug log.
func konsoleActivate(h host, service string, session int32) func(conn net.Conn, reader *bufio.Reader, token string) {
	return func(conn net.Conn, reader *bufio.Reader, _ string) {
		service, serial := busText(service), uint32(3)
		ask := func(dest, path, iface, member, signature string, body []byte, want ...string) ([]byte, error) {
			serial++
			return busCall(conn, reader, h.replyTimeout, serial, methodCall(serial, dest, path, iface, member, signature, body), want...)
		}
		window := ""
		for n := 1; n <= konsoleWindows && window == ""; n++ {
			path := "/Windows/" + strconv.Itoa(n)
			body, err := ask(service, path, konsoleWindowName, "sessionList", "", nil, "as")
			if errors.Is(err, os.ErrDeadlineExceeded) {
				log.Debug("konsole session list failed", "error", err)
				return
			}
			if err == nil && slices.Contains(decodeStrings(body), strconv.Itoa(int(session))) {
				window = path
			}
		}
		if window == "" {
			log.Debug("no konsole window has the session", "session", session)
			return
		}
		body, err := ask(service, window, konsoleWindowName, "currentSession", "", nil, "i")
		if err != nil || len(body) < 4 {
			log.Debug("konsole current tab failed", "error", err)
			return
		}
		current := strconv.Itoa(int(int32(binary.LittleEndian.Uint32(body))))
		body, err = ask(service, "/Sessions/"+current, "org.kde.konsole.Session", "title", "i", appendUint32(nil, 1), "s")
		if err != nil {
			log.Debug("konsole tab title failed", "error", err)
			return
		}
		title, _, ok := decodeString(body, 0)
		if !ok {
			log.Debug("konsole tab title has a bad body")
			return
		}
		selectTab := methodCall(serial+1, service, window, konsoleWindowName, "setCurrentSession", "i", appendUint32(nil, uint32(session)))
		selectTab[2] = noReplyNoAutoStart
		if err := busWrite(conn, h.replyTimeout, selectTab); err != nil {
			log.Debug("select konsole tab failed", "error", err)
			return
		}
		serial++
		body, err = ask(kwinName, kwinRunnerPath, krunnerName, "Match", "s", appendString(nil, "konsole"), "a(sssida{sv})", "a(sssuda{sv})")
		if err != nil {
			log.Debug("kwin window match failed", "error", err)
			return
		}
		id, found := pickWindow(decodeMatches(body), title)
		if !found {
			log.Debug("no kwin window for the konsole tab", "title", title)
			return
		}
		run := methodCall(serial+1, kwinName, kwinRunnerPath, krunnerName, "Run", "ss", appendString(appendString(nil, busText(id)), ""))
		run[2] = noReplyNoAutoStart
		if err := busWrite(conn, h.replyTimeout, run); err != nil {
			log.Debug("run kwin window failed", "error", err)
		}
	}
}

// busCall writes call with serial and NO_AUTO_START and returns the body of its return, or an error for no return, an error reply or a signature that is not in want.
func busCall(conn net.Conn, reader *bufio.Reader, timeout time.Duration, serial uint32, call []byte, want ...string) ([]byte, error) {
	call[2] = noAutoStart
	if err := busWrite(conn, timeout, call); err != nil {
		return nil, err
	}
	reply, err := busReply(conn, reader, timeout, serial)
	if err != nil {
		return nil, err
	}
	if reply.kind == kindError {
		return nil, errors.New(reply.errName)
	}
	if !slices.Contains(want, reply.signature) {
		return nil, fmt.Errorf("reply signature %q", reply.signature)
	}
	return reply.body, nil
}

// pickWindow returns the id of the first window whose caption starts with title and the dash, else with title and " [", else the only window when its caption ends with Konsole; only ids that start with "0_" are windows.
func pickWindow(matches [][2]string, title string) (string, bool) {
	var windows [][2]string
	for _, match := range matches {
		if strings.HasPrefix(match[0], "0_") {
			windows = append(windows, match)
		}
	}
	for _, prefix := range []string{title + captionDash, title + " ["} {
		for _, window := range windows {
			if strings.HasPrefix(window[1], prefix) {
				return window[0], true
			}
		}
	}
	if len(windows) == 1 && strings.HasSuffix(windows[0][1], "Konsole") {
		return windows[0][0], true
	}
	return "", false
}

// decodeMatches reads the id and the caption of each struct of a body of signature a(sssida{sv}), which KWin sends, or a(sssuda{sv}), which its introspection names; nil for a short or bad body.
func decodeMatches(body []byte) [][2]string {
	if len(body) < 8 {
		return nil
	}
	size := uint64(binary.LittleEndian.Uint32(body))
	if 8+size > uint64(len(body)) {
		return nil
	}
	b, p := body[:8+int(size)], 8
	var matches [][2]string
	for p < len(b) {
		var fields [3]string
		p = align(p, 8)
		for i := range fields {
			value, next, ok := decodeString(b, p)
			if !ok {
				return nil
			}
			fields[i], p = value, next
		}
		p = align(align(p, 4)+4, 8) + 8
		if p+4 > len(b) {
			return nil
		}
		dict := uint64(binary.LittleEndian.Uint32(b[p:]))
		p = align(p+4, 8)
		if uint64(p)+dict > uint64(len(b)) {
			return nil
		}
		p += int(dict)
		matches = append(matches, [2]string{fields[0], fields[1]})
	}
	return matches
}

// decodeStrings reads a body of signature as, or nil for a short or bad body.
func decodeStrings(body []byte) []string {
	if len(body) < 4 {
		return nil
	}
	size := uint64(binary.LittleEndian.Uint32(body))
	if 4+size > uint64(len(body)) {
		return nil
	}
	b, p := body[:4+int(size)], 4
	var values []string
	for p < len(b) {
		value, next, ok := decodeString(b, p)
		if !ok {
			return nil
		}
		values, p = append(values, value), next
	}
	return values
}

// decodeString reads the string at the first 4-byte boundary from p in b and returns it with the position after its NUL.
func decodeString(b []byte, p int) (string, int, bool) {
	p = align(p, 4)
	if p+4 > len(b) {
		return "", 0, false
	}
	size := uint64(binary.LittleEndian.Uint32(b[p:]))
	if uint64(p)+5+size > uint64(len(b)) || b[p+4+int(size)] != 0 {
		return "", 0, false
	}
	return string(b[p+4 : p+4+int(size)]), p + 5 + int(size), true
}
