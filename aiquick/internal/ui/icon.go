package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"

	"fyne.io/fyne/v2"
)

var (
	iconOnce sync.Once
	iconData []byte
)

// AppIcon 运行时生成的应用图标（紫色圆角底 + 白色闪电），
// 避免仓库内嵌二进制资源。
func AppIcon() fyne.Resource {
	iconOnce.Do(func() {
		const S = 128
		img := image.NewRGBA(image.Rect(0, 0, S, S))
		bg := color.RGBA{R: 0x6C, G: 0x5C, B: 0xE7, A: 0xFF}
		fg := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

		inRounded := func(x, y int) bool {
			const m = 6
			const r = 26
			if x < m || y < m || x >= S-m || y >= S-m {
				return false
			}
			// 点向圆角矩形内 clamp：不在角区则距离为 0（必在内部）
			cx, cy := x, y
			if x < m+r {
				cx = m + r
			} else if x > S-m-r-1 {
				cx = S - m - r - 1
			}
			if y < m+r {
				cy = m + r
			} else if y > S-m-r-1 {
				cy = S - m - r - 1
			}
			dx, dy := x-cx, y-cy
			return dx*dx+dy*dy <= r*r
		}

		bolt := []image.Point{{74, 22}, {46, 70}, {62, 70}, {52, 106}, {84, 56}, {66, 56}, {82, 22}}
		inPoly := func(px, py int) bool {
			inside := false
			j := len(bolt) - 1
			for i := 0; i < len(bolt); i++ {
				xi, yi := bolt[i].X, bolt[i].Y
				xj, yj := bolt[j].X, bolt[j].Y
				if (yi > py) != (yj > py) &&
					px < (xj-xi)*(py-yi)/(yj-yi)+xi {
					inside = !inside
				}
				j = i
			}
			return inside
		}

		for y := 0; y < S; y++ {
			for x := 0; x < S; x++ {
				switch {
				case inPoly(x, y):
					img.Set(x, y, fg)
				case inRounded(x, y):
					img.Set(x, y, bg)
				}
			}
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			iconData = buf.Bytes()
		}
	})
	return fyne.NewStaticResource("aiquick.png", iconData)
}
