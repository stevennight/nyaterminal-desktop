package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nyaterminal/nyaterminal-desktop/internal/app"
	"github.com/nyaterminal/nyaterminal-desktop/internal/version"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if version.IsVersionCommand(os.Args[1:]) {
		fmt.Println(version.Print("NyaTerminal"))
		return
	}

	if handled, exitCode, err := runHelloHelperIfRequested(os.Args[1:]); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitCode)
	}

	dataDir, err := os.UserConfigDir()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cannot find configuration directory:", err)
		os.Exit(1)
	}
	application := app.New(filepath.Join(dataDir, "NyaTerminal"))
	defer func() {
		_ = application.Close()
	}()

	err = wails.Run(&options.App{
		Title:     "NyaTerminal",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 10, G: 14, B: 22, A: 1},
		OnStartup:        application.Startup,
		OnShutdown:       application.Shutdown,
		Bind:             []interface{}{application},
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "application error:", err)
		os.Exit(1)
	}
}
