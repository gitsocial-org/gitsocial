// konsole_test.go - Tests for the Konsole exchange against a fake bus: the tab, the title, the KWin match, the retry, the fallback, the errors and the decoders.
package desktop

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"reflect"
	"strings"
	"testing"
)

// encodeMatches encodes a reply of Match by the D-Bus rules: the array length, the pad to 8, and per window at an 8-byte boundary the id, the caption, the icon, the type, the relevance and a dict with the subtext.
func encodeMatches(windows [][2]string) []byte {
	b := padTo(appendUint32(nil, 0), 8)
	start := len(b)
	for _, window := range windows {
		b = appendString(appendString(appendString(padTo(b, 8), window[0]), window[1]), "utilities-terminal")
		b = binary.LittleEndian.AppendUint64(padTo(appendUint32(b, 30), 8), math.Float64bits(0.7))
		b = appendStringVariants(b, [][2]string{{"subtext", "Activate running window on Desktop 1"}})
	}
	binary.LittleEndian.PutUint32(b, uint32(len(b)-start))
	return b
}

// matchSignature is the signature of the Match reply that KWin sends; its introspection names a uint32 in place of the int32.
const matchSignature = "a(sssida{sv})"

// konsoleAnswer returns the replies of a Konsole whose window 1 has the sessions 1 and 2 with tab 2 current and whose window 3 has session 5, and of KWin with windows; a session title is "tab" and its number.
func konsoleAnswer(windows [][2]string) func(busMessage) []byte {
	sessions := map[string][]string{"/Windows/1": {"1", "2"}, "/Windows/3": {"5"}}
	current := map[string]uint32{"/Windows/1": 2, "/Windows/3": 5}
	return func(call busMessage) []byte {
		switch call.member {
		case "sessionList":
			if list, ok := sessions[call.path]; ok {
				return reply(call.serial, "as", appendStrings(nil, list))
			}
			return errorReply(call.serial, "org.freedesktop.DBus.Error.UnknownObject")
		case "currentSession":
			return reply(call.serial, "i", appendUint32(nil, current[call.path]))
		case "title":
			return reply(call.serial, "s", appendString(nil, "tab"+strings.TrimPrefix(call.path, "/Sessions/")))
		case "Match":
			return reply(call.serial, matchSignature, encodeMatches(windows))
		}
		return nil
	}
}

// konsoleExchange clicks a message of a TUI in a Konsole session of :1.94 against a fake bus that writes the reply that answer gives for each call, and returns the calls after Notify.
func konsoleExchange(t *testing.T, session int32, answer func(busMessage) []byte) []busMessage {
	t.Helper()
	calls := make(chan busMessage, 64)
	record := answerAndRecord(calls, signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction))
	socket, _, done := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
		record(conn, call)
		if message := answer(call); message != nil {
			conn.Write(message)
		}
	})
	sent, clicked, err := dbusNotify(testHost(), socket, konsoleActivate(testHost(), ":1.94", session), "GitSocial", "body", "", clickSent)
	if !sent || !clicked || err != nil {
		t.Fatalf("dbusNotify = sent %v, clicked %v, %v; want sent, clicked and no error", sent, clicked, err)
	}
	<-done
	close(calls)
	var got []busMessage
	for call := range calls {
		got = append(got, call)
	}
	return got
}

// runID returns the window id of the Run call in calls, or "" with no Run.
func runID(t *testing.T, calls []busMessage) string {
	t.Helper()
	for _, call := range calls {
		if call.member == "Run" {
			id, _, ok := decodeString(call.body, 0)
			if !ok {
				t.Fatalf("Run body = %q, want a string first", call.body)
			}
			return id
		}
	}
	return ""
}

// members returns the member names of calls.
func members(calls []busMessage) string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.member)
	}
	return strings.Join(names, " ")
}

// describe returns the fields of a call that the tests compare.
func describe(call busMessage) string {
	return fmt.Sprintf("%s %s %s.%s(%s) serial %d flags %#x", call.destination, call.path, call.iface, call.member, call.signature, call.serial, call.flags)
}

func TestKonsoleActivate_selectsTabAndWindow(t *testing.T) {
	calls := konsoleExchange(t, 1, konsoleAnswer([][2]string{
		{"0_{6c58ccbf-1bd6-43af-bf5d-7e7c65f896e5}", "tab5 \u2014 Konsole"},
		{"0_{f327380a-adcb-4319-9af8-f198071c24e2}", "tab2 \u2014 Konsole"},
	}))
	want := []struct{ fields, body string }{
		{":1.94 /Windows/1 org.kde.konsole.Window.sessionList() serial 4 flags 0x2", ""},
		{":1.94 /Windows/1 org.kde.konsole.Window.currentSession() serial 5 flags 0x2", ""},
		{":1.94 /Sessions/2 org.kde.konsole.Session.title(i) serial 6 flags 0x2", "\x01\x00\x00\x00"},
		{":1.94 /Windows/1 org.kde.konsole.Window.setCurrentSession(i) serial 7 flags 0x3", "\x01\x00\x00\x00"},
		{"org.kde.KWin /WindowsRunner org.kde.krunner1.Match(s) serial 8 flags 0x2", "\x07\x00\x00\x00konsole\x00"},
		{"org.kde.KWin /WindowsRunner org.kde.krunner1.Run(ss) serial 9 flags 0x3", "\x28\x00\x00\x000_{f327380a-adcb-4319-9af8-f198071c24e2}\x00" + "\x00\x00\x00" + "\x00\x00\x00\x00\x00"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %q, want sessionList, currentSession, title, setCurrentSession, Match and Run", members(calls))
	}
	for i, call := range calls {
		if got := describe(call); got != want[i].fields || string(call.body) != want[i].body {
			t.Errorf("call %d = %s, body %q; want %s, body %q", i, got, call.body, want[i].fields, want[i].body)
		}
	}
}

func TestKonsoleActivate_findsTheWindowOfTheSession(t *testing.T) {
	calls := konsoleExchange(t, 5, konsoleAnswer([][2]string{{"0_one", "tab2 \u2014 Konsole"}, {"0_three", "tab5 \u2014 Konsole"}}))
	if got := members(calls); got != "sessionList sessionList sessionList currentSession title setCurrentSession Match Run" {
		t.Fatalf("calls = %q, want three sessionList before the rest", got)
	}
	if got := describe(calls[5]); got != ":1.94 /Windows/3 org.kde.konsole.Window.setCurrentSession(i) serial 9 flags 0x3" || string(calls[5].body) != "\x05\x00\x00\x00" {
		t.Errorf("tab call = %s, body %q; want session 5 on window 3", got, calls[5].body)
	}
	if got := runID(t, calls); got != "0_three" {
		t.Errorf("Run of %q, want the window with the title of tab 5", got)
	}
	calls = konsoleExchange(t, 9, konsoleAnswer([][2]string{{"0_one", "tab2 \u2014 Konsole"}}))
	if got := members(calls); got != strings.TrimSuffix(strings.Repeat("sessionList ", konsoleWindows), " ") {
		t.Errorf("calls for a session in no window = %q, want %d sessionList and no more", got, konsoleWindows)
	}
}

func TestKonsoleActivate_picksByTitle(t *testing.T) {
	for _, c := range []struct {
		name    string
		windows [][2]string
		want    string
	}{
		{"the dash after the title", [][2]string{
			{"8_desktop", "tab2 \u2014 Konsole"},
			{"0_x", "tab2 -x \u2014 Konsole"},
			{"0_tmp", "tab5 \u2014 Konsole"},
			{"0_home", "tab2 \u2014 Konsole"},
		}, "0_home"},
		{"the container after the title", [][2]string{
			{"0_tmp", "tab5 \u2014 Konsole"},
			{"0_box", "tab2 [box] \u2014 Konsole"},
		}, "0_box"},
		{"the dash before the container", [][2]string{
			{"0_box", "tab2 [box] \u2014 Konsole"},
			{"0_home", "tab2 \u2014 Konsole"},
		}, "0_home"},
	} {
		if got := runID(t, konsoleExchange(t, 1, konsoleAnswer(c.windows))); got != c.want {
			t.Errorf("%s: Run of %q, want %q", c.name, got, c.want)
		}
	}
}

func TestKonsoleActivate_fallback(t *testing.T) {
	for _, c := range []struct {
		name    string
		windows [][2]string
		want    string
	}{
		{"one Konsole window", [][2]string{{"0_tmp", "other \u2014 Konsole"}, {"8_desktop", "Desktop konsole"}}, "0_tmp"},
		{"two Konsole windows", [][2]string{{"0_tmp", "tab5 \u2014 Konsole"}, {"0_other", "other \u2014 Konsole"}}, ""},
		{"one window of another app", [][2]string{{"0_web", "konsole tips \u2014 Browser"}}, ""},
	} {
		calls := konsoleExchange(t, 1, konsoleAnswer(c.windows))
		if got := runID(t, calls); got != c.want {
			t.Errorf("%s: Run of %q, want %q", c.name, got, c.want)
		}
		if got := members(calls); !strings.HasPrefix(got, "sessionList currentSession title setCurrentSession Match") {
			t.Errorf("%s: calls = %q, want the tab call before Match", c.name, got)
		}
	}
}

func TestKonsoleActivate_errorsStop(t *testing.T) {
	good := konsoleAnswer([][2]string{{"0_home", "tab2 \u2014 Konsole"}})
	for _, c := range []struct {
		name, member string
		message      func(serial uint32) []byte
		want         string
	}{
		{"error reply to currentSession", "currentSession", func(serial uint32) []byte { return errorReply(serial, "org.freedesktop.DBus.Error.UnknownMethod") }, "sessionList currentSession"},
		{"error reply to title", "title", func(serial uint32) []byte { return errorReply(serial, "org.freedesktop.DBus.Error.UnknownMethod") }, "sessionList currentSession title"},
		{"title of another signature", "title", func(serial uint32) []byte { return reply(serial, "u", appendUint32(nil, 1)) }, "sessionList currentSession title"},
		{"KWin absent", "Match", func(serial uint32) []byte { return errorReply(serial, "org.freedesktop.DBus.Error.ServiceUnknown") }, "sessionList currentSession title setCurrentSession Match"},
		{"Match of another signature", "Match", func(serial uint32) []byte { return reply(serial, "as", appendStrings(nil, []string{"0_home"})) }, "sessionList currentSession title setCurrentSession Match"},
	} {
		calls := konsoleExchange(t, 1, func(call busMessage) []byte {
			if call.member == c.member {
				return c.message(call.serial)
			}
			return good(call)
		})
		if got := members(calls); got != c.want {
			t.Errorf("%s: calls = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestKonsoleActivate_introspectedMatchSignature(t *testing.T) {
	good := konsoleAnswer([][2]string{{"0_home", "tab2 \u2014 Konsole"}})
	calls := konsoleExchange(t, 1, func(call busMessage) []byte {
		if call.member == "Match" {
			return reply(call.serial, "a(sssuda{sv})", encodeMatches([][2]string{{"0_home", "tab2 \u2014 Konsole"}}))
		}
		return good(call)
	})
	if got := runID(t, calls); got != "0_home" {
		t.Errorf("Run of %q, want the window for the signature of the introspection too", got)
	}
}

func TestDecodeStrings_forms(t *testing.T) {
	if got := decodeStrings(appendStrings(nil, []string{"1", "12"})); !reflect.DeepEqual(got, []string{"1", "12"}) {
		t.Errorf("decodeStrings = %q, want 1 and 12", got)
	}
	if got := decodeStrings([]byte("\x06\x00\x00\x00\x01\x00\x00\x007\x00")); !reflect.DeepEqual(got, []string{"7"}) {
		t.Errorf("decodeStrings of a hand-written body = %q, want 7", got)
	}
	if got := decodeStrings(make([]byte, 4)); len(got) != 0 {
		t.Errorf("decodeStrings of an empty array = %q, want none", got)
	}
	for name, body := range map[string][]byte{
		"nil":                   nil,
		"length past the body":  []byte("\x09\x00\x00\x00\x01\x00\x00\x007\x00"),
		"string past the array": []byte("\x06\x00\x00\x00\x02\x00\x00\x007\x00"),
		"string with no NUL":    []byte("\x06\x00\x00\x00\x01\x00\x00\x0077"),
	} {
		if got := decodeStrings(body); got != nil {
			t.Errorf("%s: decodeStrings = %q, want nil", name, got)
		}
	}
}

func TestKonsoleSession_forms(t *testing.T) {
	for _, c := range []struct {
		service, path string
		want          int32
		ok            bool
	}{
		{":1.94", "/Sessions/1", 1, true},
		{":1.94", "/Sessions/12", 12, true},
		{"org.kde.konsole-4242", "/Sessions/2147483647", 2147483647, true},
		{"", "/Sessions/1", 0, false},
		{":1.94", "", 0, false},
		{":1.94", "/Sessions/", 0, false},
		{":1.94", "/Sessions/0", 0, false},
		{":1.94", "/Sessions/-1", 0, false},
		{":1.94", "/Sessions/+1", 0, false},
		{":1.94", "/Sessions/2147483648", 0, false},
		{":1.94", "/Sessions/1/2", 0, false},
		{":1.94", "/Windows/1", 0, false},
		{":1.94", "Sessions/1", 0, false},
	} {
		if got, ok := konsoleSession(c.service, c.path); got != c.want || ok != c.ok {
			t.Errorf("konsoleSession(%q, %q) = %d, %v; want %d, %v", c.service, c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestDecodeMatches_forms(t *testing.T) {
	oneStruct := "\x30\x00\x00\x00" + "\x00\x00\x00\x00" +
		"\x03\x00\x00\x00" + "0_a\x00" + "\x01\x00\x00\x00" + "x\x00" + "\x00\x00" + "\x00\x00\x00\x00" + "\x00" + "\x00\x00\x00" +
		"\x01\x00\x00\x00" + "\x00\x00\x00\x00" + "\x00\x00\x00\x00\x00\x00\xe0\x3f" + "\x00\x00\x00\x00" + "\x00\x00\x00\x00"
	if got := decodeMatches([]byte(oneStruct)); !reflect.DeepEqual(got, [][2]string{{"0_a", "x"}}) {
		t.Errorf("decodeMatches of one struct = %q, want 0_a and x", got)
	}
	windows := [][2]string{
		{"0_{f327380a-adcb-4319-9af8-f198071c24e2}", "~ : bash \u2014 Konsole"},
		{"0_{6c58ccbf-1bd6-43af-bf5d-7e7c65f896e5}", "tmp : bash \u2014 Konsole"},
	}
	twoWindows := encodeMatches(windows)
	if got := decodeMatches(twoWindows); !reflect.DeepEqual(got, windows) {
		t.Errorf("decodeMatches of two windows = %q, want %q", got, windows)
	}
	if got := decodeMatches(make([]byte, 8)); len(got) != 0 {
		t.Errorf("decodeMatches of an empty array = %q, want none", got)
	}
	noNUL := []byte(oneStruct)
	noNUL[15] = 'x'
	cutStruct := []byte(oneStruct)
	cutStruct[0] = 0x2f
	longString := []byte(oneStruct)
	copy(longString[8:], "\xff\xff\xff\xff")
	longDict := []byte(oneStruct)
	copy(longDict[48:], "\x09\x00\x00\x00")
	for name, body := range map[string][]byte{
		"nil":                       nil,
		"short length":              {0x30, 0, 0},
		"length past the body":      []byte(oneStruct)[:40],
		"string with no NUL":        noNUL,
		"array end inside a struct": cutStruct,
		"string past the body":      longString,
		"dict past the array":       longDict,
	} {
		if got := decodeMatches(body); got != nil {
			t.Errorf("%s: decodeMatches = %q, want nil", name, got)
		}
	}
	for n := range twoWindows {
		decodeMatches(twoWindows[:n])
		changed := append([]byte(nil), twoWindows...)
		changed[n] = 0xff
		decodeMatches(changed)
	}
}
