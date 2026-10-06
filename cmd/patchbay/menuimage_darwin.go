//go:build tray

package main

// menuimage_darwin.m keeps menu item images visible on macOS 27.

// #cgo LDFLAGS: -framework AppKit
// long pbMenuImageVisibility(void);
import "C"

// menuImageVisibility is the preferredImageVisibility AppKit reports for a
// menu item given an image: 1 is visible, -1 means the OS predates it.
func menuImageVisibility() int { return int(C.pbMenuImageVisibility()) }
