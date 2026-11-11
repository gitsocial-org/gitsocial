// dbus_test.go - Tests for the session bus client against a fake bus: the click, the close, dropped messages, errors, the Notify fields, the Activate call and the bus address.
package desktop

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testID is the id that the fake bus gives each message.
const testID uint32 = 42

// fakeBus listens on a short socket directory for one client, answers its auth with authReply and gives each method call to handle; it returns the socket, a channel with the auth line and a channel that closes when the fake stops reading.
func fakeBus(t *testing.T, authReply string, handle func(net.Conn, busMessage)) (string, chan string, chan struct{}) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake bus is a Unix socket")
	}
	dir, err := os.MkdirTemp("/tmp", "gsbus")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "bus")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	auth := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		auth <- line
		conn.Write([]byte(authReply))
		if !strings.HasPrefix(authReply, "OK ") {
			io.Copy(io.Discard, reader)
			return
		}
		if _, err := reader.ReadString('\n'); err != nil {
			return
		}
		for {
			call, ok, err := readBusMessage(reader)
			if err != nil {
				return
			}
			if ok && call.kind == kindCall {
				handle(conn, call)
			}
		}
	}()
	return socket, auth, done
}

// useBus points the session bus variables at socket.
func useBus(t *testing.T, socket string) {
	t.Helper()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+socket)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Dir(socket))
}

// answerSetup answers Hello and AddMatch and reports whether call was one of them.
func answerSetup(conn net.Conn, call busMessage) bool {
	switch call.member {
	case "Hello":
		conn.Write(reply(call.serial, "s", appendString(nil, ":1.7")))
	case "AddMatch":
		conn.Write(reply(call.serial, "", nil))
	default:
		return false
	}
	return true
}

// answerNotify returns a handler that answers Notify with testID, sends the messages and closes the connection.
func answerNotify(messages ...[]byte) func(net.Conn, busMessage) {
	return func(conn net.Conn, call busMessage) {
		if answerSetup(conn, call) {
			return
		}
		conn.Write(reply(call.serial, "u", appendUint32(nil, testID)))
		for _, m := range messages {
			conn.Write(m)
		}
		conn.Close()
	}
}

// answerAndRecord returns a handler that answers Notify with testID, sends the messages, keeps the connection open and gives each later call to calls.
func answerAndRecord(calls chan busMessage, messages ...[]byte) func(net.Conn, busMessage) {
	return func(conn net.Conn, call busMessage) {
		if answerSetup(conn, call) {
			return
		}
		if call.member != "Notify" {
			calls <- call
			return
		}
		conn.Write(reply(call.serial, "u", appendUint32(nil, testID)))
		for _, m := range messages {
			conn.Write(m)
		}
	}
}

// ptyxisCall builds the Activate call of Ptyxis, the call of a TUI message in that terminal.
func ptyxisCall(token string) []byte {
	return activateCall(4, "org.gnome.Ptyxis", appPath("org.gnome.Ptyxis"), token)
}

// clickSent is an onClick that succeeds.
func clickSent() error {
	return nil
}

// assertNoCall fails the test when the fake got a call after Notify; it waits for the end of the fake first.
func assertNoCall(t *testing.T, name string, calls chan busMessage, done chan struct{}) {
	t.Helper()
	<-done
	select {
	case call := <-calls:
		t.Errorf("%s: call %s.%s after the click, want none", name, call.iface, call.member)
	default:
	}
}

// appendReplySerial appends the REPLY_SERIAL header field.
func appendReplySerial(b []byte, serial uint32) []byte {
	return appendUint32(appendSignature(append(padTo(b, 8), fieldReplySerial), "u"), serial)
}

// reply returns the method return for serial.
func reply(serial uint32, signature string, body []byte) []byte {
	fields := appendReplySerial(nil, serial)
	if signature != "" {
		fields = appendStringField(fields, fieldSignature, "g", signature)
	}
	return encodeMessage(kindReturn, 100+serial, fields, body)
}

// errorReply returns the error reply for serial.
func errorReply(serial uint32, name string) []byte {
	return encodeMessage(kindError, 100+serial, appendStringField(appendReplySerial(nil, serial), fieldErrorName, "s", name), nil)
}

// signalFields returns the header fields of a signal of the notification service.
func signalFields(member, signature string) []byte {
	fields := appendStringField(nil, fieldPath, "o", notificationsPath)
	fields = appendStringField(fields, fieldInterface, "s", notificationsName)
	fields = appendStringField(fields, fieldMember, "s", member)
	return appendStringField(fields, fieldSignature, "g", signature)
}

// signalUS returns a signal with an id and a string.
func signalUS(member string, id uint32, value string) []byte {
	return encodeMessage(kindSignal, 200, signalFields(member, "us"), appendString(appendUint32(nil, id), value))
}

// closedSignal returns NotificationClosed for id.
func closedSignal(id uint32) []byte {
	return encodeMessage(kindSignal, 201, signalFields("NotificationClosed", "uu"), appendUint32(appendUint32(nil, id), 2))
}

// bigEndian returns message with the big-endian flag and its lengths and serial in big-endian order.
func bigEndian(message []byte) []byte {
	b := append([]byte{'B'}, message[1:]...)
	for _, at := range []int{4, 8, 12} {
		binary.BigEndian.PutUint32(b[at:], binary.LittleEndian.Uint32(message[at:]))
	}
	return b
}

// unknownFieldClick returns a click for testID with a header field of type uint64 first.
func unknownFieldClick() []byte {
	fields := appendSignature([]byte{10}, "t")
	fields = append(padTo(fields, 8), 1, 2, 3, 4, 5, 6, 7, 8)
	fields = append(fields, signalFields("ActionInvoked", "us")...)
	return encodeMessage(kindSignal, 202, fields, appendString(appendUint32(nil, testID), clickAction))
}

func TestDBusNotify_click(t *testing.T) {
	socket, _, _ := fakeBus(t, "OK 0123\r\n", answerNotify(signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction), closedSignal(testID)))
	sent, clicked, err := dbusNotify(testHost(), socket, nil, "GitSocial", "body", "gitsocial", clickSent)
	if !sent || !clicked || err != nil {
		t.Errorf("dbusNotify = sent %v, clicked %v, %v; want sent and clicked", sent, clicked, err)
	}
}

func TestDBusNotify_closedAndIgnored(t *testing.T) {
	socket, _, _ := fakeBus(t, "OK 0123\r\n", answerNotify(
		signalUS("ActionInvoked", 7, clickAction),
		signalUS("ActivationToken", testID, "token"),
		signalUS("ActionInvoked", testID, "Open"),
		bigEndian(signalUS("ActionInvoked", testID, clickAction)),
		unknownFieldClick(),
		closedSignal(7),
		closedSignal(testID),
		signalUS("ActionInvoked", testID, clickAction),
	))
	sent, clicked, err := dbusNotify(testHost(), socket, nil, "GitSocial", "body", "", clickSent)
	if !sent || clicked || err != nil {
		t.Errorf("dbusNotify = sent %v, clicked %v, %v; want sent, no click and no error", sent, clicked, err)
	}
}

func TestDBusNotify_errors(t *testing.T) {
	if sent, _, err := dbusNotify(testHost(), "", nil, "GitSocial", "body", "", clickSent); sent || err == nil {
		t.Errorf("dbusNotify with no address = sent %v, %v; want not sent", sent, err)
	}
	notSent := map[string]func(*testing.T) string{
		"refused auth": func(t *testing.T) string {
			socket, _, _ := fakeBus(t, "REJECTED EXTERNAL\r\n", nil)
			return socket
		},
		"error reply to Hello": func(t *testing.T) string {
			socket, _, _ := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
				conn.Write(errorReply(call.serial, "org.freedesktop.DBus.Error.AccessDenied"))
			})
			return socket
		},
		"error reply to Notify": func(t *testing.T) string {
			socket, _, _ := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
				if !answerSetup(conn, call) {
					conn.Write(errorReply(call.serial, "org.freedesktop.DBus.Error.AccessDenied"))
				}
			})
			return socket
		},
	}
	for name, bus := range notSent {
		if sent, clicked, err := dbusNotify(testHost(), bus(t), nil, "GitSocial", "body", "", clickSent); sent || clicked || err == nil {
			t.Errorf("%s: dbusNotify = sent %v, clicked %v, %v; want not sent and an error", name, sent, clicked, err)
		}
	}
	shortHeader := encodeMessage(kindSignal, 203, []byte{fieldMember, 5, 's'}, nil)
	declaresMore := closedSignal(testID)[:20]
	shortBody := encodeMessage(kindSignal, 204, signalFields("ActionInvoked", "us"), appendUint32(nil, testID))
	for name, messages := range map[string][][]byte{"closed connection": nil, "short header": {shortHeader}, "short message": {declaresMore}, "short body": {shortBody}} {
		socket, _, _ := fakeBus(t, "OK 0123\r\n", answerNotify(messages...))
		if sent, clicked, err := dbusNotify(testHost(), socket, nil, "GitSocial", "body", "", clickSent); !sent || clicked || err == nil {
			t.Errorf("%s: dbusNotify = sent %v, clicked %v, %v; want sent, no click and an error", name, sent, clicked, err)
		}
	}
}

func TestDBusNotify_messageFields(t *testing.T) {
	calls := make(chan busMessage, 1)
	socket, auth, _ := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
		if !answerSetup(conn, call) {
			calls <- call
			answerNotify(closedSignal(testID))(conn, call)
		}
	})
	if sent, _, err := dbusNotify(testHost(), socket, nil, "Git\xffSocial", "<b>&\xfe", "gitsocial", clickSent); !sent || err != nil {
		t.Fatalf("dbusNotify = sent %v, %v; want sent", sent, err)
	}
	if line, want := <-auth, "\x00AUTH EXTERNAL "+hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))+"\r\n"; line != want {
		t.Errorf("auth line = %q, want %q", line, want)
	}
	call := <-calls
	if call.iface != notificationsName || call.member != "Notify" || call.signature != "susssasa{sv}i" {
		t.Errorf("call = %s.%s(%s), want %s.Notify(susssasa{sv}i)", call.iface, call.member, call.signature, notificationsName)
	}
	b, p := call.body, 0
	u32 := func() uint32 {
		p = align(p, 4)
		v := binary.LittleEndian.Uint32(b[p:])
		p += 4
		return v
	}
	str := func() string {
		n := int(u32())
		v := string(b[p : p+n])
		p += n + 1
		return v
	}
	app, replaces, icon, title, body := str(), u32(), str(), str(), str()
	end := p + int(u32())
	var actions []string
	for p < end {
		actions = append(actions, str())
	}
	hints := int(u32())
	p = align(p, 8) + hints
	expire := u32()
	if app != "GitSocial" || replaces != 0 || icon != "gitsocial" {
		t.Errorf("app, replaces, icon = %q, %d, %q; want GitSocial, 0, gitsocial", app, replaces, icon)
	}
	if title != "Git�Social" || body != "&lt;b&gt;&amp;�" {
		t.Errorf("title, body = %q, %q; want valid UTF-8 and the escaped body", title, body)
	}
	if strings.Join(actions, "|") != "default|Open" || hints != 0 || expire != 0xFFFFFFFF || p != len(b) {
		t.Errorf("actions %q, hints %d, expire %#x, end %d of %d; want default and Open, no hints, 0xFFFFFFFF", actions, hints, expire, p, len(b))
	}
}

func TestDBusNotify_replyDeadline(t *testing.T) {
	socket, _, _ := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) { answerSetup(conn, call) })
	h := testHost()
	h.replyTimeout = 200 * time.Millisecond
	start := time.Now()
	sent, clicked, err := dbusNotify(h, socket, nil, "GitSocial", "body", "", clickSent)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("dbusNotify returned after %v, want the end of the wait at the reply deadline", elapsed)
	}
	if !sent || clicked || err == nil {
		t.Errorf("dbusNotify = sent %v, clicked %v, %v; want sent after the Notify write, no click and an error", sent, clicked, err)
	}
}

func TestBusAddress_forms(t *testing.T) {
	for _, c := range []struct{ value, runtimeDir, want string }{
		{"unix:path=/run/user/1000/bus", "", "/run/user/1000/bus"},
		{"unix:abstract=/tmp/dbus-0123", "", "@/tmp/dbus-0123"},
		{"unix:path=/run/user/1000/bus,guid=0123abcd", "/run/user/1000", "/run/user/1000/bus"},
		{"unix:guid=0123abcd,path=/run/bus", "", "/run/bus"},
		{"tcp:host=localhost,port=1234;unix:path=/run/bus;unix:path=/run/other", "", "/run/bus"},
		{"unix:path=/tmp/a%20b%2cc", "", "/tmp/a b,c"},
		{"unix:path=/tmp/%zz", "", ""},
		{"", "/run/user/1000", "/run/user/1000/bus"},
		{"", "", ""},
		{"tcp:host=localhost,port=1234", "/run/user/1000", ""},
	} {
		if got := busAddress(c.value, c.runtimeDir); got != c.want {
			t.Errorf("busAddress(%q, %q) = %q, want %q", c.value, c.runtimeDir, got, c.want)
		}
	}
}

func TestDBusNotify_activatesTerminal(t *testing.T) {
	calls := make(chan busMessage, 4)
	socket, _, done := fakeBus(t, "OK 0123\r\n", answerAndRecord(calls, signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction)))
	clicks := 0
	onClick := func() error {
		clicks++
		return nil
	}
	sent, clicked, err := dbusNotify(testHost(), socket, ptyxisCall, "GitSocial", "body", "", onClick)
	if !sent || !clicked || err != nil || clicks != 1 {
		t.Fatalf("dbusNotify = sent %v, clicked %v, %v, %d clicks; want sent, clicked and one onClick", sent, clicked, err, clicks)
	}
	<-done
	select {
	case call := <-calls:
		if call.destination != "org.gnome.Ptyxis" || call.path != "/org/gnome/Ptyxis" || call.iface != applicationName || call.member != "Activate" || call.signature != "a{sv}" {
			t.Errorf("call = %s %s %s.%s(%s), want Activate of org.gnome.Ptyxis", call.destination, call.path, call.iface, call.member, call.signature)
		}
		if call.flags != noReplyNoAutoStart {
			t.Errorf("flags = %#x, want %#x", call.flags, noReplyNoAutoStart)
		}
		if n := strings.Count(string(call.body), "\x05\x00\x00\x00token\x00"); n != 2 {
			t.Errorf("token in the body %d times, want 2", n)
		}
	default:
		t.Fatal("no Activate after the click")
	}
	assertNoCall(t, "a second call", calls, done)
}

func TestDBusNotify_noActivateAfterFailedClick(t *testing.T) {
	calls := make(chan busMessage, 4)
	socket, _, done := fakeBus(t, "OK 0123\r\n", answerAndRecord(calls, signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction)))
	failed := errors.New("dial click socket: no such file")
	sent, clicked, err := dbusNotify(testHost(), socket, ptyxisCall, "GitSocial", "body", "", func() error { return failed })
	if !sent || !clicked || !errors.Is(err, failed) {
		t.Errorf("dbusNotify = sent %v, clicked %v, %v; want sent, clicked and the error of onClick", sent, clicked, err)
	}
	assertNoCall(t, "failed onClick", calls, done)
}

func TestDBusNotify_noActivate(t *testing.T) {
	for _, c := range []struct {
		name     string
		activate func(token string) []byte
		messages [][]byte
	}{
		{"no call", nil, [][]byte{signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction)}},
		{"no token", ptyxisCall, [][]byte{signalUS("ActionInvoked", testID, clickAction)}},
	} {
		calls := make(chan busMessage, 4)
		socket, _, done := fakeBus(t, "OK 0123\r\n", answerAndRecord(calls, c.messages...))
		if sent, clicked, err := dbusNotify(testHost(), socket, c.activate, "GitSocial", "body", "", clickSent); !sent || !clicked || err != nil {
			t.Errorf("%s: dbusNotify = sent %v, clicked %v, %v; want sent and clicked", c.name, sent, clicked, err)
		}
		assertNoCall(t, c.name, calls, done)
	}
}

func TestDBusNotify_tokenOfOtherMessage(t *testing.T) {
	calls := make(chan busMessage, 4)
	socket, _, done := fakeBus(t, "OK 0123\r\n", answerAndRecord(calls, signalUS("ActivationToken", 7, "other"), signalUS("ActionInvoked", testID, clickAction)))
	if sent, clicked, err := dbusNotify(testHost(), socket, ptyxisCall, "GitSocial", "body", "", clickSent); !sent || !clicked || err != nil {
		t.Errorf("dbusNotify = sent %v, clicked %v, %v; want sent and clicked", sent, clicked, err)
	}
	assertNoCall(t, "token of another message", calls, done)
	calls = make(chan busMessage, 4)
	socket, _, done = fakeBus(t, "OK 0123\r\n", answerAndRecord(calls, signalUS("ActivationToken", testID, "mine"), signalUS("ActivationToken", 7, "other"), signalUS("ActionInvoked", testID, clickAction)))
	if _, _, err := dbusNotify(testHost(), socket, ptyxisCall, "GitSocial", "body", "", clickSent); err != nil {
		t.Fatalf("dbusNotify: %v", err)
	}
	<-done
	select {
	case call := <-calls:
		if !strings.Contains(string(call.body), "mine") || strings.Contains(string(call.body), "other") {
			t.Errorf("Activate body = %q, want the token of this message", call.body)
		}
	default:
		t.Error("no Activate with the token of this message")
	}
}

func TestActivateCall_bytes(t *testing.T) {
	b := activateCall(4, "org.gnome.Ptyxis", "/org/gnome/Ptyxis", "to\x00k1")
	if b[0] != 'l' || b[1] != kindCall || b[2] != 0x3 || b[3] != 1 || binary.LittleEndian.Uint32(b[8:]) != 4 {
		t.Errorf("header = %q, want little-endian, a call, flags 0x3, version 1 and serial 4", b[:12])
	}
	m, ok, err := readBusMessage(bytes.NewReader(b))
	if err != nil || !ok {
		t.Fatalf("readBusMessage = %v, %v", ok, err)
	}
	if m.destination != "org.gnome.Ptyxis" || m.path != "/org/gnome/Ptyxis" || m.iface != "org.freedesktop.Application" || m.member != "Activate" || m.signature != "a{sv}" {
		t.Errorf("fields = %s %s %s.%s(%s), want Activate of org.gnome.Ptyxis", m.destination, m.path, m.iface, m.member, m.signature)
	}
	want := "\x4d\x00\x00\x00" + "\x00\x00\x00\x00" +
		"\x10\x00\x00\x00activation-token\x00" + "\x01s\x00" + "\x04\x00\x00\x00tok1\x00" + "\x00\x00\x00\x00\x00\x00\x00" +
		"\x12\x00\x00\x00desktop-startup-id\x00" + "\x01s\x00" + "\x00\x00" + "\x04\x00\x00\x00tok1\x00"
	if string(m.body) != want {
		t.Errorf("body = %q, want %q", m.body, want)
	}
	if got := binary.LittleEndian.Uint32(b[4:]); got != uint32(len(want)) {
		t.Errorf("body length = %d, want %d", got, len(want))
	}
}

func TestActivateResultCall_bytes(t *testing.T) {
	b := activateResultCall(5, ":1.272", "40a8cc32-b75e-4b5e-a9b7-95dd6d352c3d")
	if b[2] != 0x3 || binary.LittleEndian.Uint32(b[8:]) != 5 {
		t.Errorf("header = %q, want flags 0x3 and serial 5", b[:12])
	}
	m, ok, err := readBusMessage(bytes.NewReader(b))
	if err != nil || !ok {
		t.Fatalf("readBusMessage = %v, %v", ok, err)
	}
	if m.destination != ":1.272" || m.path != "/org/gnome/Terminal/SearchProvider" || m.iface != "org.gnome.Shell.SearchProvider2" || m.member != "ActivateResult" || m.signature != "sasu" {
		t.Errorf("fields = %s %s %s.%s(%s), want ActivateResult of the GNOME Terminal search provider", m.destination, m.path, m.iface, m.member, m.signature)
	}
	want := "\x24\x00\x00\x0040a8cc32-b75e-4b5e-a9b7-95dd6d352c3d\x00" + "\x00\x00\x00" + "\x00\x00\x00\x00" + "\x00\x00\x00\x00"
	if string(m.body) != want {
		t.Errorf("body = %q, want %q", m.body, want)
	}
}

func TestPresentSurfaceCall_bytes(t *testing.T) {
	b := presentSurfaceCall(4, "com.mitchellh.ghostty", 0xa2680fb67d38fbf2, "tok1")
	if b[2] != 0x3 || binary.LittleEndian.Uint32(b[8:]) != 4 {
		t.Errorf("header = %q, want flags 0x3 and serial 4", b[:12])
	}
	m, ok, err := readBusMessage(bytes.NewReader(b))
	if err != nil || !ok {
		t.Fatalf("readBusMessage = %v, %v", ok, err)
	}
	if m.destination != "com.mitchellh.ghostty" || m.path != "/com/mitchellh/ghostty" || m.iface != "org.freedesktop.Application" || m.member != "ActivateAction" || m.signature != "sava{sv}" {
		t.Errorf("fields = %s %s %s.%s(%s), want ActivateAction of com.mitchellh.ghostty", m.destination, m.path, m.iface, m.member, m.signature)
	}
	want := "\x0f\x00\x00\x00present-surface\x00" +
		"\x10\x00\x00\x00" + "\x01t\x00" + "\x00\x00\x00\x00\x00" + "\xf2\xfb\x38\x7d\xb6\x0f\x68\xa2" +
		"\x4d\x00\x00\x00" + "\x00\x00\x00\x00" +
		"\x10\x00\x00\x00activation-token\x00" + "\x01s\x00" + "\x04\x00\x00\x00tok1\x00" + "\x00\x00\x00\x00\x00\x00\x00" +
		"\x12\x00\x00\x00desktop-startup-id\x00" + "\x01s\x00" + "\x00\x00" + "\x04\x00\x00\x00tok1\x00"
	if string(m.body) != want {
		t.Errorf("body = %q, want %q", m.body, want)
	}
}

func TestAppPath_forms(t *testing.T) {
	for app, want := range map[string]string{
		"org.gnome.Ptyxis":       "/org/gnome/Ptyxis",
		"org.gnome.Ptyxis-Devel": "/org/gnome/Ptyxis_Devel",
		"com.example.my-app.Two": "/com/example/my_app/Two",
	} {
		if got := appPath(app); got != want {
			t.Errorf("appPath(%q) = %q, want %q", app, got, want)
		}
	}
}
