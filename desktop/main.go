package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Исправление известного бага WebKitGTK на Wayland + Nvidia (Error 71 Protocol error)
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "NetTurn — Next-Gen VK Calls TURN Tunnel",
		Width:     960,
		Height:    680,
		MinWidth:  800,
		MinHeight: 580,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 11, G: 15, B: 25, A: 1}, // Deep obsidian dark
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Linux: &linux.Options{
			Icon:                nil,
			WindowIsTranslucent: false,
			WebviewGpuPolicy:    linux.WebviewGpuPolicyOnDemand,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
