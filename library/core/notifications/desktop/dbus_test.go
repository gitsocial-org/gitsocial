// dbus_test.go - Tests for the session bus client against a fake bus: the click, the close, dropped messages, errors, the Notify fields and the bus address.
package desktop

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
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

// fakeBus listens on a short socket directory for one client, answers its auth with authReply and gives each method call to handle; it returns the socket and a channel with the auth line.
func fakeBus(t *testing.T, authReply string, handle func(net.Conn, busMessage)) (string, chan string) {
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
	go func() {
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
	return socket, auth
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
	socket, _ := fakeBus(t, "OK 0123\r\n", answerNotify(signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction), closedSignal(testID)))
	sent, clicked, err := dbusNotify(testHost(), socket, "GitSocial", "body", "gitsocial")
	if !sent || !clicked || err != nil {
		t.Errorf("dbusNotify = sent %v, clicked %v, %v; want sent and clicked", sent, clicked, err)
	}
}

func TestDBusNotify_closedAndIgnored(t *testing.T) {
	socket, _ := fakeBus(t, "OK 0123\r\n", answerNotify(
		signalUS("ActionInvoked", 7, clickAction),
		signalUS("ActivationToken", testID, "token"),
		signalUS("ActionInvoked", testID, "Open"),
		bigEndian(signalUS("ActionInvoked", testID, clickAction)),
		unknownFieldClick(),
		closedSignal(7),
		closedSignal(testID),
		signalUS("ActionInvoked", testID, clickAction),
	))
	sent, clicked, err := dbusNotify(testHost(), socket, "GitSocial", "body", "")
	if !sent || clicked || err != nil {
		t.Errorf("dbusNotify = sent %v, clicked %v, %v; want sent, no click and no error", sent, clicked, err)
	}
}

func TestDBusNotify_errors(t *testing.T) {
	if sent, _, err := dbusNotify(testHost(), "", "GitSocial", "body", ""); sent || err == nil {
		t.Errorf("dbusNotify with no address = sent %v, %v; want not sent", sent, err)
	}
	notSent := map[string]func(*testing.T) string{
		"refused auth": func(t *testing.T) string {
			socket, _ := fakeBus(t, "REJECTED EXTERNAL\r\n", nil)
			return socket
		},
		"error reply to Hello": func(t *testing.T) string {
			socket, _ := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
				conn.Write(errorReply(call.serial, "org.freedesktop.DBus.Error.AccessDenied"))
			})
			return socket
		},
		"error reply to Notify": func(t *testing.T) string {
			socket, _ := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
				if !answerSetup(conn, call) {
					conn.Write(errorReply(call.serial, "org.freedesktop.DBus.Error.AccessDenied"))
				}
			})
			return socket
		},
	}
	for name, bus := range notSent {
		if sent, clicked, err := dbusNotify(testHost(), bus(t), "GitSocial", "body", ""); sent || clicked || err == nil {
			t.Errorf("%s: dbusNotify = sent %v, clicked %v, %v; want not sent and an error", name, sent, clicked, err)
		}
	}
	shortHeader := encodeMessage(kindSignal, 203, []byte{fieldMember, 5, 's'}, nil)
	declaresMore := closedSignal(testID)[:20]
	shortBody := encodeMessage(kindSignal, 204, signalFields("ActionInvoked", "us"), appendUint32(nil, testID))
	for name, messages := range map[string][][]byte{"closed connection": nil, "short header": {shortHeader}, "short message": {declaresMore}, "short body": {shortBody}} {
		socket, _ := fakeBus(t, "OK 0123\r\n", answerNotify(messages...))
		if sent, clicked, err := dbusNotify(testHost(), socket, "GitSocial", "body", ""); !sent || clicked || err == nil {
			t.Errorf("%s: dbusNotify = sent %v, clicked %v, %v; want sent, no click and an error", name, sent, clicked, err)
		}
	}
}

func TestDBusNotify_messageFields(t *testing.T) {
	calls := make(chan busMessage, 1)
	socket, auth := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
		if !answerSetup(conn, call) {
			calls <- call
			answerNotify(closedSignal(testID))(conn, call)
		}
	})
	if sent, _, err := dbusNotify(testHost(), socket, "Git\xffSocial", "<b>&\xfe", "gitsocial"); !sent || err != nil {
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
	socket, _ := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) { answerSetup(conn, call) })
	h := testHost()
	h.replyTimeout = 200 * time.Millisecond
	start := time.Now()
	sent, clicked, err := dbusNotify(h, socket, "GitSocial", "body", "")
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
