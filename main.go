package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var embedded embed.FS

func main() {
	frontend, err := fs.Sub(embedded, "frontend")
	if err != nil {
		log.Fatal(err)
	}

	app := NewApp()
	err = wails.Run(&options.App{
		Title:            "VC Desk",
		Width:            1180,
		Height:           800,
		MinWidth:         900,
		MinHeight:        650,
		AssetServer:      &assetserver.Options{Assets: frontend},
		BackgroundColour: &options.RGBA{R: 18, G: 22, B: 32, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
