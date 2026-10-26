package main

import (
	"os"
	"path/filepath"
	"time"

	webview2 "github.com/jchv/go-webview2"

	"skillscanner/internal/config"
	"skillscanner/internal/ui"
)

func main() {
	dir := config.Dir()
	srv := ui.New(dir)
	url, err := srv.Start()
	if err != nil {
		os.Exit(1)
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		// Portable: browser data is kept next to the exe, not in AppData.
		DataPath: filepath.Join(dir, "data", "webview"),
		WindowOptions: webview2.WindowOptions{
			Title:  "SkillScanner - GitHub Skill Report",
			Width:  980,
			Height: 820,
			Center: true,
		},
	})
	if w == nil {
		// No WebView2: open the interface in the default browser. Exit once the
		// tab has been silent for 2 minutes and no scan is running.
		ui.Open(url)
		start := time.Now()
		for range time.Tick(10 * time.Second) {
			idle, busy := srv.Idle()
			if busy {
				continue
			}
			if (idle == 0 && time.Since(start) > 3*time.Minute) || idle > 2*time.Minute {
				os.Exit(0)
			}
		}
	}
	defer w.Destroy()
	w.SetSize(980, 820, webview2.HintNone)
	w.Navigate(url)
	w.Run()
}
