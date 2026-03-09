//go:build !windows

package main

import "context"

func startTray(_ context.Context, _ *App) {
	// System tray is only supported on Windows.
}
