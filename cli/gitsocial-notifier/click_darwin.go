//go:build darwin && cgo

// click_darwin.go - The exported function that the delegate of a clicked notification calls; a file with an export holds no C definitions.
package main

import "C"

import "github.com/gitsocial-org/gitsocial/library/core/notifications/click"

// gsClick decides the click of a notification and returns 1 when the delegate opens its URL.
//
//export gsClick
func gsClick(url, socket, ref *C.char) C.int {
	if clickOpensURL(C.GoString(url), C.GoString(socket), C.GoString(ref), click.Send) {
		return 1
	}
	return 0
}
