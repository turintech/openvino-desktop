//go:build windows

package main

import (
	"context"
	_ "embed"
	"os"
	goruntime "runtime"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed logo.ico
var trayIcon []byte

func startTray(ctx context.Context, app *App) {
	goruntime.LockOSThread()
	systray.Run(
		func() {
			systray.SetIcon(trayIcon)
			systray.SetTooltip("Turintech - OpenVINO Desktop")

			mShow := systray.AddMenuItem("Show", "Open the window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Exit OpenVINO Desk")

			for {
				select {
				case <-mShow.ClickedCh:
					runtime.WindowShow(ctx)
					runtime.WindowUnminimise(ctx)
				case <-mQuit.ClickedCh:
					app.shutdown()
					systray.Quit()
					os.Exit(0)
				}
			}
		},
		func() {},
	)
}
