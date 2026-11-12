// dbus.go - A session bus client that shows one desktop message with a click action and waits for its click or its close.
package desktop

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
)

const (
	busName           = "org.freedesktop.DBus"
	busPath           = "/org/freedesktop/DBus"
	notificationsName = "org.freedesktop.Notifications"
	notificationsPath = "/org/freedesktop/Notifications"
	applicationName   = "org.freedesktop.Application"
)

const (
	kindCall   byte = 1
	kindReturn byte = 2
	kindError  byte = 3
	kindSignal byte = 4
)

const (
	fieldPath        byte = 1
	fieldInterface   byte = 2
	fieldMember      byte = 3
	fieldErrorName   byte = 4
	fieldReplySerial byte = 5
	fieldDestination byte = 6
	fieldSignature   byte = 8
)

// noReplyNoAutoStart is the header flags NO_REPLY_EXPECTED and NO_AUTO_START.
const noReplyNoAutoStart byte = 0x1 | 0x2

// noAutoStart is the header flag NO_AUTO_START alone, for a call that waits for its reply.
const noAutoStart byte = 0x2

// maxBody and maxFields bound the parts of a message that the client reads; a larger message is dropped.
const maxBody, maxFields = 1 << 20, 64 << 10

// busMessage is the part of a received message that the client reads.
type busMessage struct {
	kind        byte
	flags       byte
	serial      uint32
	replySerial uint32
	path        string
	destination string
	iface       string
	member      string
	errName     string
	signature   string
	body        []byte
}

// busAddress returns the socket of the session bus from the value of DBUS_SESSION_BUS_ADDRESS, with "@" before an abstract name, else runtimeDir/bus, else "".
func busAddress(value, runtimeDir string) string {
	if value == "" {
		if runtimeDir == "" {
			return ""
		}
		return runtimeDir + "/bus"
	}
	for _, entry := range strings.Split(value, ";") {
		transport, keys, _ := strings.Cut(entry, ":")
		if transport != "unix" {
			continue
		}
		for _, key := range strings.Split(keys, ",") {
			name, raw, _ := strings.Cut(key, "=")
			if name != "path" && name != "abstract" {
				continue
			}
			socket, err := url.PathUnescape(raw)
			if err != nil || socket == "" {
				return ""
			}
			if name == "abstract" {
				return "@" + socket
			}
			return socket
		}
		return ""
	}
	return ""
}

// dbusNotify shows one message with the click action on the bus at address and waits for its click, its close or waitTimeout; on a click it runs onClick and, after a good onClick with a token, runs the activate exchange on the connection. sent is false when the service did not get or refused the message.
func dbusNotify(h host, address string, activate func(conn net.Conn, reader *bufio.Reader, token string), title, body, icon string, onClick func() error) (sent, clicked bool, err error) {
	if address == "" {
		return false, false, errors.New("no session bus address")
	}
	conn, err := net.DialTimeout("unix", address, h.replyTimeout)
	if err != nil {
		return false, false, fmt.Errorf("dial session bus: %w", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	if err := busAuth(conn, reader, h.replyTimeout); err != nil {
		return false, false, err
	}
	rule := "type='signal',interface='" + notificationsName + "',path='" + notificationsPath + "'"
	if err := busSetupCall(conn, reader, h.replyTimeout, 1, "Hello", "", nil); err != nil {
		return false, false, err
	}
	if err := busSetupCall(conn, reader, h.replyTimeout, 2, "AddMatch", "s", appendString(nil, rule)); err != nil {
		return false, false, err
	}
	notify := methodCall(3, notificationsName, notificationsPath, notificationsName, "Notify", "susssasa{sv}i", notifyBody(title, body, icon))
	if err := busWrite(conn, h.replyTimeout, notify); err != nil {
		return false, false, err
	}
	reply, err := busReply(conn, reader, h.replyTimeout, 3)
	if err != nil {
		return true, false, fmt.Errorf("notify: %w", err)
	}
	if reply.kind == kindError {
		return false, false, fmt.Errorf("notify: %s", reply.errName)
	}
	if reply.signature != "u" || len(reply.body) < 4 {
		return true, false, errors.New("notify: reply has no id")
	}
	clicked, token, err := busWaitClick(conn, reader, binary.LittleEndian.Uint32(reply.body), time.Now().Add(waitTimeout))
	if err != nil || !clicked {
		return true, clicked, err
	}
	if err := onClick(); err != nil {
		return true, true, err
	}
	if activate != nil && token != "" {
		activate(conn, reader, token)
	}
	return true, true, nil
}

// writeCalls returns the exchange that writes the calls that build makes from the token and reads no reply.
func writeCalls(h host, build func(token string) []byte) func(conn net.Conn, reader *bufio.Reader, token string) {
	return func(conn net.Conn, _ *bufio.Reader, token string) {
		if err := busWrite(conn, h.replyTimeout, build(token)); err != nil {
			log.Debug("activate terminal failed", "error", err)
		}
	}
}

// busAuth runs the EXTERNAL auth with the uid of this process and starts the message stream.
func busAuth(conn net.Conn, reader *bufio.Reader, timeout time.Duration) error {
	uid := hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))
	if err := busWrite(conn, timeout, []byte("\x00AUTH EXTERNAL "+uid+"\r\n")); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set bus deadline: %w", err)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read bus auth: %w", err)
	}
	if !strings.HasPrefix(line, "OK ") {
		return fmt.Errorf("bus auth refused: %s", strings.TrimSpace(line))
	}
	return busWrite(conn, timeout, []byte("BEGIN\r\n"))
}

// busSetupCall calls a method of the bus and waits for its return.
func busSetupCall(conn net.Conn, reader *bufio.Reader, timeout time.Duration, serial uint32, member, signature string, body []byte) error {
	if err := busWrite(conn, timeout, methodCall(serial, busName, busPath, busName, member, signature, body)); err != nil {
		return err
	}
	reply, err := busReply(conn, reader, timeout, serial)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.ToLower(member), err)
	}
	if reply.kind == kindError {
		return fmt.Errorf("%s: %s", strings.ToLower(member), reply.errName)
	}
	return nil
}

// busWrite writes one message within timeout.
func busWrite(conn net.Conn, timeout time.Duration, message []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set bus deadline: %w", err)
	}
	if _, err := conn.Write(message); err != nil {
		return fmt.Errorf("write to session bus: %w", err)
	}
	return nil
}

// busReply reads until the return or the error for serial, each read within timeout, and drops the messages between.
func busReply(conn net.Conn, reader *bufio.Reader, timeout time.Duration, serial uint32) (busMessage, error) {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return busMessage{}, fmt.Errorf("set bus deadline: %w", err)
		}
		m, ok, err := readBusMessage(reader)
		if err != nil {
			return busMessage{}, err
		}
		if ok && (m.kind == kindReturn || m.kind == kindError) && m.replySerial == serial {
			return m, nil
		}
	}
}

// busWaitClick reads until ActionInvoked with the click action or NotificationClosed for id, or until deadline, and reports a click with the last ActivationToken for id.
func busWaitClick(conn net.Conn, reader *bufio.Reader, id uint32, deadline time.Time) (bool, string, error) {
	if err := conn.SetReadDeadline(deadline); err != nil {
		return false, "", fmt.Errorf("set bus deadline: %w", err)
	}
	token := ""
	for {
		m, ok, err := readBusMessage(reader)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return false, "", nil
		}
		if err != nil {
			return false, "", fmt.Errorf("wait for click: %w", err)
		}
		if !ok || m.kind != kindSignal || m.iface != notificationsName {
			continue
		}
		if (m.member == "ActionInvoked" || m.member == "ActivationToken") && m.signature == "us" {
			signalID, value, err := decodeUintString(m.body)
			if err != nil {
				return false, "", err
			}
			if signalID == id && m.member == "ActivationToken" {
				token = value
			}
			if signalID == id && m.member == "ActionInvoked" && value == clickAction {
				return true, token, nil
			}
		}
		if m.member == "NotificationClosed" && m.signature == "uu" {
			if len(m.body) < 8 {
				return false, "", errors.New("short NotificationClosed signal")
			}
			if binary.LittleEndian.Uint32(m.body) == id {
				return false, "", nil
			}
		}
	}
}

// readBusMessage reads one message; ok is false for a message that is read to its end and dropped: big-endian, too large, or with an unknown header field type.
func readBusMessage(reader io.Reader) (busMessage, bool, error) {
	head := make([]byte, 16)
	if _, err := io.ReadFull(reader, head); err != nil {
		return busMessage{}, false, fmt.Errorf("read message: %w", err)
	}
	var order binary.ByteOrder = binary.LittleEndian
	switch head[0] {
	case 'l':
	case 'B':
		order = binary.BigEndian
	default:
		return busMessage{}, false, fmt.Errorf("message byte order %q", head[0])
	}
	bodyLen, fieldsLen := int64(order.Uint32(head[4:])), int64(order.Uint32(head[12:]))
	padded := (fieldsLen + 7) &^ 7
	if head[0] != 'l' || bodyLen > maxBody || fieldsLen > maxFields {
		if _, err := io.CopyN(io.Discard, reader, padded+bodyLen); err != nil {
			return busMessage{}, false, fmt.Errorf("drop message: %w", err)
		}
		return busMessage{}, false, nil
	}
	rest := make([]byte, padded+bodyLen)
	if _, err := io.ReadFull(reader, rest); err != nil {
		return busMessage{}, false, fmt.Errorf("read message: %w", err)
	}
	m := busMessage{kind: head[1], flags: head[2], serial: binary.LittleEndian.Uint32(head[8:]), body: rest[padded:]}
	return parseHeaderFields(rest[:fieldsLen], m)
}

// parseHeaderFields reads the header fields into m; ok is false for an unknown field type.
func parseHeaderFields(fields []byte, m busMessage) (busMessage, bool, error) {
	short := errors.New("short message header")
	for p := 0; p < len(fields); p = align(p, 8) {
		if p+2 > len(fields) || p+3+int(fields[p+1]) > len(fields) {
			return m, false, short
		}
		code, n := fields[p], int(fields[p+1])
		signature := string(fields[p+2 : p+2+n])
		p += 3 + n
		switch signature {
		case "s", "o":
			p = align(p, 4)
			if p+4 > len(fields) {
				return m, false, short
			}
			size := uint64(binary.LittleEndian.Uint32(fields[p:]))
			if uint64(p)+5+size > uint64(len(fields)) {
				return m, false, short
			}
			value := string(fields[p+4 : p+4+int(size)])
			p += 5 + int(size)
			switch code {
			case fieldPath:
				m.path = value
			case fieldDestination:
				m.destination = value
			case fieldInterface:
				m.iface = value
			case fieldMember:
				m.member = value
			case fieldErrorName:
				m.errName = value
			}
		case "g":
			if p+1 > len(fields) || p+2+int(fields[p]) > len(fields) {
				return m, false, short
			}
			if code == fieldSignature {
				m.signature = string(fields[p+1 : p+1+int(fields[p])])
			}
			p += 2 + int(fields[p])
		case "u":
			p = align(p, 4)
			if p+4 > len(fields) {
				return m, false, short
			}
			if code == fieldReplySerial {
				m.replySerial = binary.LittleEndian.Uint32(fields[p:])
			}
			p += 4
		default:
			return m, false, nil
		}
	}
	return m, true, nil
}

// decodeUintString reads the body of signature "us".
func decodeUintString(body []byte) (uint32, string, error) {
	if len(body) < 8 {
		return 0, "", errors.New("short signal body")
	}
	size := uint64(binary.LittleEndian.Uint32(body[4:]))
	if 8+size > uint64(len(body)) {
		return 0, "", errors.New("short signal body")
	}
	return binary.LittleEndian.Uint32(body), string(body[8 : 8+int(size)]), nil
}

// notifyBody returns the arguments of Notify: the app name, no id to replace, the icon, the title, the escaped body, the click action, no hints and the default expiry.
func notifyBody(title, body, icon string) []byte {
	b := appendString(nil, "GitSocial")
	b = appendUint32(b, 0)
	b = appendString(b, icon)
	b = appendString(b, busText(title))
	b = appendString(b, escapeMarkup(busText(body)))
	b = appendStrings(b, []string{clickAction, "Open"})
	b = padTo(appendUint32(b, 0), 8)
	return appendUint32(b, 0xFFFFFFFF)
}

// activateCall returns the Activate call of the application at path on dest with token as its activation token, with no reply and no auto start.
func activateCall(serial uint32, dest, path, token string) []byte { //nolint:unparam
	token = busText(token)
	b := methodCall(serial, busText(dest), path, applicationName, "Activate", "a{sv}", appendStringVariants(nil, [][2]string{{"activation-token", token}, {"desktop-startup-id", token}}))
	b[2] = noReplyNoAutoStart
	return b
}

// activateResultCall returns the ActivateResult call that asks the GNOME Terminal server dest to select and present the tab with uuid, with no reply and no auto start.
func activateResultCall(serial uint32, dest, uuid string) []byte {
	body := appendUint32(appendStrings(appendString(nil, busText(uuid)), nil), 0)
	b := methodCall(serial, busText(dest), "/org/gnome/Terminal/SearchProvider", "org.gnome.Shell.SearchProvider2", "ActivateResult", "sasu", body)
	b[2] = noReplyNoAutoStart
	return b
}

// presentSurfaceCall returns the ActivateAction call that asks the application app to present its surface with token, with no reply and no auto start.
func presentSurfaceCall(serial uint32, app string, surface uint64, token string) []byte {
	token = busText(token)
	body := appendUint32(appendString(nil, "present-surface"), 0)
	start := len(body)
	body = binary.LittleEndian.AppendUint64(padTo(appendSignature(body, "t"), 8), surface)
	binary.LittleEndian.PutUint32(body[start-4:], uint32(len(body)-start))
	body = appendStringVariants(body, [][2]string{{"activation-token", token}, {"desktop-startup-id", token}})
	b := methodCall(serial, app, appPath(app), applicationName, "ActivateAction", "sava{sv}", body)
	b[2] = noReplyNoAutoStart
	return b
}

// appPath returns the object path of an application from its bus name: "." to "/" and "-" to "_".
func appPath(app string) string {
	return "/" + strings.NewReplacer(".", "/", "-", "_").Replace(app)
}

// appendStringVariants appends a dict of signature a{sv} whose values are variants of strings.
func appendStringVariants(b []byte, entries [][2]string) []byte {
	b = appendUint32(b, 0)
	at := len(b) - 4
	b = padTo(b, 8)
	start := len(b)
	for _, entry := range entries {
		b = appendString(padTo(b, 8), entry[0])
		b = appendString(appendSignature(b, "s"), entry[1])
	}
	binary.LittleEndian.PutUint32(b[at:], uint32(len(b)-start))
	return b
}

// busText returns s as a string that the bus accepts: valid UTF-8 with no NUL.
func busText(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, "\ufffd"), "\x00", "")
}

// methodCall returns one method call to dest with a body of signature.
func methodCall(serial uint32, dest, path, iface, member, signature string, body []byte) []byte {
	fields := appendStringField(nil, fieldPath, "o", path)
	fields = appendStringField(fields, fieldInterface, "s", iface)
	fields = appendStringField(fields, fieldMember, "s", member)
	fields = appendStringField(fields, fieldDestination, "s", dest)
	if signature != "" {
		fields = appendStringField(fields, fieldSignature, "g", signature)
	}
	return encodeMessage(kindCall, serial, fields, body)
}

// encodeMessage returns one little-endian message of kind with the encoded header fields and the body.
func encodeMessage(kind byte, serial uint32, fields, body []byte) []byte {
	b := []byte{'l', kind, 0, 1}
	b = appendUint32(b, uint32(len(body)))
	b = appendUint32(b, serial)
	b = appendUint32(b, uint32(len(fields)))
	return append(padTo(append(b, fields...), 8), body...)
}

// appendStringField appends one header field whose value is a string, an object path or a signature.
func appendStringField(b []byte, code byte, signature, value string) []byte {
	b = appendSignature(append(padTo(b, 8), code), signature)
	if signature == "g" {
		return appendSignature(b, value)
	}
	return appendString(b, value)
}

// appendStrings appends an array of strings.
func appendStrings(b []byte, values []string) []byte {
	b = appendUint32(b, 0)
	start := len(b)
	for _, value := range values {
		b = appendString(b, value)
	}
	binary.LittleEndian.PutUint32(b[start-4:], uint32(len(b)-start))
	return b
}

// appendString appends a string with its length and its terminating NUL.
func appendString(b []byte, s string) []byte {
	return append(append(appendUint32(b, uint32(len(s))), s...), 0)
}

// appendSignature appends a signature with its one-byte length and its terminating NUL.
func appendSignature(b []byte, s string) []byte {
	return append(append(append(b, byte(len(s))), s...), 0)
}

// appendUint32 appends v aligned to 4.
func appendUint32(b []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(padTo(b, 4), v)
}

// padTo appends NUL bytes until the length of b is a multiple of n.
func padTo(b []byte, n int) []byte {
	for len(b)%n != 0 {
		b = append(b, 0)
	}
	return b
}

// align returns p rounded up to a multiple of n.
func align(p, n int) int {
	return (p + n - 1) / n * n
}
