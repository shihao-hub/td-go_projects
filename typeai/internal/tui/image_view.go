package tui

import (
	"fmt"
	"strings"

	"typeai/internal/service"
)

func imageMarker(path string) string {
	return "[[image:" + path + "]]"
}

func renderImageMetadata(images []service.Image, width int) string {
	if len(images) == 0 {
		return ""
	}
	lines := make([]string, 0, len(images))
	for i, image := range images {
		lines = append(lines, dimStyle.Render(fmt.Sprintf(
			"[%d] %s · %s · %.1f KiB · %s",
			i+1,
			image.FileName,
			image.MediaType,
			float64(image.Size)/1024,
			truncateRunes(image.Path, max(12, width/2)),
		)))
	}
	return strings.Join(lines, "\n")
}
