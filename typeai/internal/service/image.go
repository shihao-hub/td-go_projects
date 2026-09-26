package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"typeai/internal/clipboard"
	"typeai/internal/llm"
	"typeai/internal/session"
)

const (
	// MaxImageSize 是原始文件上限；base64 后请求体积约为它的 4/3。
	MaxImageSize = 10 << 20
	// MaxImagesPerMessage 保守兼容常见 OpenAI-compatible 供应商。
	MaxImagesPerMessage = 4
)

var (
	imageMarkerPattern     = regexp.MustCompile(`\[\[image:([^\]\r\n]+)\]\]`)
	horizontalSpacePattern = regexp.MustCompile(`[ \t]+`)
	newlineSpacePattern    = regexp.MustCompile(` ?\n ?`)
	excessNewlinePattern   = regexp.MustCompile(`\n{2,}`)
)

// Image 是一次请求使用的本地图片负载和来源元数据。
type Image struct {
	Path      string
	FileName  string
	MediaType string
	Size      int64
	Base64    string
}

// PrepareImage 读取并校验本地图；只返回内存负载，不产生临时文件。
func PrepareImage(path string) (Image, error) {
	expanded, err := expandImagePath(path)
	if err != nil {
		return Image{}, err
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return Image{}, coded("invalid_image", fmt.Sprintf("读取图片失败: %v", err))
	}
	if info.IsDir() {
		return Image{}, coded("invalid_image", "图片路径是目录")
	}
	if info.Size() == 0 {
		return Image{}, coded("invalid_image", "图片文件为空")
	}
	if info.Size() > MaxImageSize {
		return Image{}, coded("invalid_image", fmt.Sprintf("图片超过 %d MiB 上限", MaxImageSize>>20))
	}

	raw, err := os.ReadFile(expanded)
	if err != nil {
		return Image{}, coded("invalid_image", fmt.Sprintf("读取图片失败: %v", err))
	}
	mediaType, err := detectImageMediaType(raw)
	if err != nil {
		return Image{}, err
	}
	return Image{
		Path:      expanded,
		FileName:  info.Name(),
		MediaType: mediaType,
		Size:      info.Size(),
		Base64:    base64.StdEncoding.EncodeToString(raw),
	}, nil
}

func detectImageMediaType(raw []byte) (string, error) {
	switch detected := http.DetectContentType(raw); detected {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return detected, nil
	default:
		return "", coded("invalid_image", "仅支持 PNG、JPEG、WebP 或 GIF 图片")
	}
}

// ParseImageMarkers validates image references and returns model text plus payloads.
func ParseImageMarkers(input string) (string, []Image, error) {
	matches := imageMarkerPattern.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(input), nil, nil
	}

	images := make([]Image, 0, len(matches))
	for _, match := range matches {
		image, err := PrepareImage(match[1])
		if err != nil {
			return "", nil, err
		}
		images = append(images, image)
	}
	return stripImageMarkers(input), images, nil
}

func stripImageMarkers(input string) string {
	cleaned := imageMarkerPattern.ReplaceAllString(input, "\x00")
	lines := strings.Split(cleaned, "\n")
	for i, line := range lines {
		withoutMarker := strings.ReplaceAll(line, "\x00", "")
		if strings.TrimSpace(withoutMarker) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = strings.TrimSpace(horizontalSpacePattern.ReplaceAllString(strings.ReplaceAll(line, "\x00", " "), " "))
	}
	cleaned = strings.Join(lines, "\n")
	cleaned = newlineSpacePattern.ReplaceAllString(cleaned, "\n")
	cleaned = excessNewlinePattern.ReplaceAllString(cleaned, "\n")
	return strings.TrimSpace(cleaned)
}

// CaptureClipboardImage saves the clipboard image to the temporary project
// directory and validates its payload.
func CaptureClipboardImage() (Image, error) {
	path, err := clipboard.CaptureImage()
	if err != nil {
		return Image{}, mapClipboardError(err)
	}
	return PrepareImage(path)
}

func mapClipboardError(err error) error {
	switch {
	case errors.Is(err, clipboard.ErrEmpty):
		return coded("clipboard_empty", "剪贴板中没有图片")
	case errors.Is(err, clipboard.ErrUnavailable):
		return coded("clipboard_unavailable", "当前平台无法读取剪贴板图片")
	default:
		return coded("clipboard_unavailable", "读取剪贴板图片失败")
	}
}

func loadImage(path string, expectedSize int64, expectedType string) (llm.Image, error) {
	image, err := PrepareImage(path)
	if err != nil {
		return llm.Image{}, err
	}
	if image.Size != expectedSize || image.MediaType != expectedType {
		return llm.Image{}, coded("invalid_image", fmt.Sprintf("图片在会话期间发生变化: %s", path))
	}
	return llm.Image{Base64Data: image.Base64, MediaType: image.MediaType}, nil
}

func imageMetadata(images []Image) []session.Image {
	if len(images) == 0 {
		return nil
	}
	result := make([]session.Image, 0, len(images))
	for _, image := range images {
		result = append(result, session.Image{
			Path:      image.Path,
			FileName:  image.FileName,
			MediaType: image.MediaType,
			Size:      image.Size,
		})
	}
	return result
}

func expandImagePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", coded("invalid_image", "图片路径不能为空")
	}
	if len(trimmed) >= 2 && (trimmed[0] == '"' || trimmed[0] == '`') && trimmed[0] == trimmed[len(trimmed)-1] {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, `~\`) || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", coded("invalid_image", fmt.Sprintf("定位用户目录失败: %v", err))
		}
		relative := strings.TrimLeft(strings.TrimPrefix(trimmed, "~"), `/\`)
		trimmed = filepath.Join(home, relative)
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", coded("invalid_image", fmt.Sprintf("解析图片路径失败: %v", err))
	}
	return filepath.Clean(absolute), nil
}
