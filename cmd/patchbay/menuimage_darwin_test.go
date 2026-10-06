//go:build tray

package main

import "testing"

func TestMenuImagesStayVisible(t *testing.T) {
	switch v := menuImageVisibility(); v {
	case -1:
		t.Skip("preferredImageVisibility needs macOS 27")
	case 1:
	default:
		t.Fatalf("menu item image visibility = %d, want 1 (visible)", v)
	}
}
