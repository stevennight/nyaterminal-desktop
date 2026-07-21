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
	windowsoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
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
		BackgroundColour: &options.RGBA{R: 10, G: 14, B: 22, A: 255},
		Windows: &windowsoptions.Options{
			Theme: windowsoptions.Dark,
			CustomTheme: &windowsoptions.ThemeSettings{
				DarkModeTitleBar:           0x00201610, // #101620
				DarkModeTitleBarInactive:   0x00281C14, // #141C28
				DarkModeTitleText:          0x00EEE3DC, // #DCE3EE
				DarkModeTitleTextInactive:  0x00A28D7F, // #7F8DA2
				DarkModeBorder:             0x00443023, // #233044
				DarkModeBorderInactive:     0x00443023, // #233044
				LightModeTitleBar:          0x00FFFFFF, // #FFFFFF
				LightModeTitleBarInactive:  0x00F6F2EE, // #EEF2F6
				LightModeTitleText:         0x002D2017, // #17202D
				LightModeTitleTextInactive: 0x008A7566, // #66758A
				LightModeBorder:            0x00E9E0D8, // #D8E0E9
				LightModeBorderInactive:    0x00E9E0D8, // #D8E0E9
			},
		},
		OnStartup:  application.Startup,
		OnShutdown: application.Shutdown,
		Bind:       []interface{}{application},
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "application error:", err)
		os.Exit(1)
	}
}
