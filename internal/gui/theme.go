// Package gui — 自定义暗色主题（OCI 品牌色）
package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// OCITheme OCI 品牌暗色主题
type OCITheme struct{}

var _ fyne.Theme = (*OCITheme)(nil)

var (
	ociRed    = color.NRGBA{R: 199, G: 70, B: 52, A: 255}  // OCI 主色
	darkBG    = color.NRGBA{R: 14, G: 14, B: 20, A: 255}   // 背景
	cardBG    = color.NRGBA{R: 22, G: 22, B: 32, A: 255}   // 卡片背景
	inputBG   = color.NRGBA{R: 28, G: 28, B: 40, A: 255}   // 输入框
	fgColor   = color.NRGBA{R: 220, G: 220, B: 235, A: 255} // 前景文字
	dimColor  = color.NRGBA{R: 90, G: 90, B: 110, A: 255}  // 占位符
	scrollBar = color.NRGBA{R: 55, G: 55, B: 75, A: 255}   // 滚动条
)

func (t *OCITheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return ociRed
	case theme.ColorNameBackground:
		return darkBG
	case theme.ColorNameButton:
		return cardBG
	case theme.ColorNameForeground:
		return fgColor
	case theme.ColorNameInputBackground:
		return inputBG
	case theme.ColorNamePlaceHolder:
		return dimColor
	case theme.ColorNameScrollBar:
		return scrollBar
	case theme.ColorNameShadow:
		return color.NRGBA{A: 80}
	case theme.ColorNameHeaderBackground:
		return cardBG
	case theme.ColorNameMenuBackground:
		return cardBG
	case theme.ColorNameOverlayBackground:
		return cardBG
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 80, G: 200, B: 120, A: 255}
	case theme.ColorNameWarning:
		return color.NRGBA{R: 230, G: 160, B: 40, A: 255}
	case theme.ColorNameError:
		return color.NRGBA{R: 220, G: 60, B: 60, A: 255}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (t *OCITheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *OCITheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *OCITheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 7
	case theme.SizeNameInnerPadding:
		return 9
	case theme.SizeNameText:
		return 13
	}
	return theme.DefaultTheme().Size(name)
}
