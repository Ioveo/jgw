// cmd/gui/main.go — GUI 版本入口（无控制台窗口）
// 构建命令: go build -ldflags="-H windowsgui" -o oci-grabber-gui.exe ./cmd/gui/
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	igui "oci-grabber/internal/gui"
)

func main() {
	a := app.NewWithID("com.jgw.oci-grabber")
	a.Settings().SetTheme(&igui.OCITheme{})

	w := a.NewWindow("OCI 甲骨文自动抢机器 v1.0")
	w.Resize(fyne.NewSize(1080, 680))
	w.CenterOnScreen()

	guiApp := igui.NewGUIApp(w)
	w.SetContent(guiApp.Build())

	w.SetCloseIntercept(func() {
		guiApp.Stop()
		w.Close()
	})

	w.ShowAndRun()
}
