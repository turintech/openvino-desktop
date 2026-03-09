package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var webAssets embed.FS

//go:embed all:assets
var embeddedAssets embed.FS

func main() {
	app := NewApp(embeddedAssets)

	err := wails.Run(&options.App{
		Title:  "Turintech - OpenVINO Desktop",
		Width:  900,
		Height: 650,
		AssetServer: &assetserver.Options{
			Assets: webAssets,
		},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			go startTray(ctx, app)
		},
		// Hide the window instead of quitting when the user clicks the close button.
		OnBeforeClose: func(ctx context.Context) bool {
			runtime.WindowHide(ctx)
			return true // true = cancel the close event
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
