package pixel

import (
	"fmt"
	"html/template"
	"io"
	"path"
	"strings"
)

// 1x1 transparent PNG (67 bytes)
var transparentPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x62, 0x00, 0x00, 0x00, 0x02,
	0x00, 0x01, 0xE5, 0x27, 0xDE, 0xFC, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// 1x1 transparent GIF (43 bytes)
var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x21, 0xF9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2C, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3B,
}

var ogTemplate = template.Must(template.New("og").Parse(`<!DOCTYPE html>
<html>
<head>
<meta property="og:title" content="Shared Link" />
<meta property="og:image" content="https://{{.Host}}/pixel.png" />
<meta property="og:type" content="website" />
</head>
<body>
<img src="https://{{.Host}}/pixel.png" width="1" height="1" />
</body>
</html>`))

type ogData struct {
	Host string
}

const (
	extGIF = ".gif"

	contentTypeGIF = "image/gif"
	contentTypePNG = "image/png"
)

func IsPixelPath(reqPath string) bool {
	ext := strings.ToLower(path.Ext(reqPath))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == extGIF
}

// ContentType returns the Content-Type matching the actual pixel bytes served.
// JPG/JPEG requests get image/png since we serve a PNG pixel for all non-GIF formats.
func ContentType(reqPath string) string {
	ext := strings.ToLower(path.Ext(reqPath))
	switch ext {
	case extGIF:
		return contentTypeGIF
	default:
		return contentTypePNG
	}
}

// WritePixel writes a 1x1 transparent image matching the requested extension.
// JPG/JPEG get a PNG response since a valid 1x1 JPEG is larger and
// browsers/clients handle the mismatch gracefully for tracking pixels.
// Callers should use ContentType() separately if they need a matching header.
func WritePixel(w io.Writer, reqPath string) error {
	ext := strings.ToLower(path.Ext(reqPath))
	switch ext {
	case extGIF:
		if _, err := w.Write(transparentGIF); err != nil {
			return fmt.Errorf("write gif pixel: %w", err)
		}

		return nil
	default:
		if _, err := w.Write(transparentPNG); err != nil {
			return fmt.Errorf("write png pixel: %w", err)
		}

		return nil
	}
}

func WriteOGPage(w io.Writer, host string) error {
	if err := ogTemplate.Execute(w, ogData{Host: host}); err != nil {
		return fmt.Errorf("render og page: %w", err)
	}

	return nil
}
