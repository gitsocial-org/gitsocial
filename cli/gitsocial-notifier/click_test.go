// click_test.go - Tests for the decision of a click: the socket of the sender, else the URL.
package main

import (
	"errors"
	"testing"
)

// recordSend returns a sender that records each socket and ref, and fails with err.
func recordSend(err error) (func(socket, ref string) error, *[]string) {
	var sent []string
	return func(socket, ref string) error {
		sent = append(sent, socket+" "+ref)
		return err
	}, &sent
}

func TestClickAction_socketOnly(t *testing.T) {
	for _, sendErr := range []error{nil, errors.New("dial click socket: no such file")} {
		send, sent := recordSend(sendErr)
		if clickOpensURL("https://site.example/", "/tmp/click/1-2.sock", "#commit:0123456789ab", send) {
			t.Errorf("a click with a socket and a send error %v opens the URL, want no URL", sendErr)
		}
		if len(*sent) != 1 || (*sent)[0] != "/tmp/click/1-2.sock #commit:0123456789ab" {
			t.Errorf("sent = %q, want the ref to the socket once", *sent)
		}
	}
}

func TestClickAction_emptyURL(t *testing.T) {
	send, sent := recordSend(nil)
	if clickOpensURL("", "", "", send) {
		t.Error("a click with no socket and no URL opens a URL, want nothing")
	}
	if !clickOpensURL("https://site.example/", "", "#commit:0123456789ab", send) {
		t.Error("a click with no socket does not open its URL")
	}
	if len(*sent) != 0 {
		t.Errorf("sent = %q with no socket, want nothing", *sent)
	}
}
