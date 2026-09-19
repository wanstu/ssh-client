package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	desktopkit "github.com/wanstu/wails-desktop-kit"
	theme "github.com/wanstu/wails-desktop-kit-theme"
)

//go:embed all:frontend
var embeddedFrontend embed.FS

//go:embed assets/appicon.png
var appIcon []byte

func main() {
	launch, err := desktopkit.ParseLaunchOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ssh-client:", err)
		os.Exit(1)
	}
	app, err := NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ssh-client:", err)
		os.Exit(1)
	}
	if err := runDesktop(app, launch); err != nil {
		fmt.Fprintln(os.Stderr, "ssh-client:", err)
		os.Exit(1)
	}
}

func runDesktop(app *App, launch desktopkit.LaunchOptions) error {
	assets, err := fs.Sub(embeddedFrontend, "frontend")
	if err != nil {
		return err
	}
	window := desktopkit.DefaultWindowConfig()
	window.Width = 1280
	window.Height = 820
	window.MinWidth = 900
	window.MinHeight = 620
	window.HidePolicy = desktopkit.HideSafe
	window.StartHiddenOnAutoStart = true
	window.Background = desktopkit.Color{R: 9, G: 13, B: 18, A: 1}

	disconnectAll := desktopkit.Action("断开全部 SSH 会话", func(*desktopkit.Controller) error { app.DisconnectAll(); return nil })

	return desktopkit.Run(desktopkit.Config{
		ID:             "ssh-client-v1",
		Title:          "SSH Client",
		Assets:         theme.MountWithKit(assets),
		Bind:           []interface{}{app},
		Launch:         launch,
		Window:         window,
		SingleInstance: true,
		Tray: desktopkit.TrayConfig{
			Enabled: true, Icon: appIcon, AutoStart: app.launchAtLogin,
			Tooltip: "SSH Client", LaunchAtLoginLabel: "开机启动 SSH Client",
			Items:     []desktopkit.TrayItem{disconnectAll},
			QuitLabel: "退出 SSH Client",
		},
		Hooks: desktopkit.Hooks{Startup: app.startup, Shutdown: app.shutdown},
	})
}
