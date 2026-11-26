package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nyaterminal/nyaterminal-desktop/internal/app"
	"github.com/nyaterminal/nyaterminal-desktop/internal/updatecheck"
	"github.com/nyaterminal/nyaterminal-desktop/internal/version"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	macoptions "github.com/wailsapp/wails/v2/pkg/options/mac"
	windowsoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/public/nyaterminal-icon-dark-gray.png
var appIcon []byte

func main() {
	args := os.Args[1:]
	if version.IsVersionCommand(args) {
		fmt.Println(version.Print("NyaTerminal"))
		return
	}

	if handled, exitCode, err := runHelloHelperIfRequested(args); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitCode)
	}

	if handled, exitCode, err := updatecheck.RunHelperIfRequested(args); handled {
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

	appOptions := &options.App{
		Title:     "NyaTerminal",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 10, G: 14, B: 22, A: 255},
		Menu:             buildApplicationMenu(application),
		Mac: &macoptions.Options{
			TitleBar:             macoptions.TitleBarDefault(),
			Appearance:           macoptions.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &macoptions.AboutInfo{
				Title:   "NyaTerminal",
				Message: version.Print("NyaTerminal"),
				Icon:    appIcon,
			},
		},
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
	}
	if !newWindowRequested(args) {
		appOptions.SingleInstanceLock = &options.SingleInstanceLock{
			UniqueId: "nyaterminal",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				application.ShowMainWindow()
			},
		}
	}

	err = wails.Run(appOptions)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "application error:", err)
		os.Exit(1)
	}
}

func buildApplicationMenu(application *app.App) *menu.Menu {
	if runtime.GOOS != "darwin" {
		return nil
	}
	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())
	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("New Window", keys.Combo("n", keys.CmdOrCtrlKey, keys.ShiftKey), func(_ *menu.CallbackData) {
		_ = application.OpenNewWindow()
	})
	appMenu.Append(menu.EditMenu())
	appMenu.Append(menu.WindowMenu())
	return appMenu
}

func newWindowRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--new-window" {
			return true
		}
	}
	return false
}
