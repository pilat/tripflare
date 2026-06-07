package pixel

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsPixelPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/img.png", true},
		{"/photo.jpg", true},
		{"/photo.jpeg", true},
		{"/anim.gif", true},
		{"/page.html", false},
		{"/", false},
		{"/img.PNG", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsPixelPath(tt.path); got != tt.want {
				t.Errorf("IsPixelPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestContentType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/img.png", "image/png"},
		{"/photo.jpg", "image/png"},
		{"/photo.jpeg", "image/png"},
		{"/anim.gif", "image/gif"},
		{"/unknown.bmp", "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ContentType(tt.path); got != tt.want {
				t.Errorf("ContentType(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestWritePixel(t *testing.T) {
	tests := []struct {
		path  string
		magic []byte
	}{
		{"/img.png", []byte{0x89, 0x50, 0x4E, 0x47}},
		{"/photo.jpg", []byte{0x89, 0x50, 0x4E, 0x47}},
		{"/anim.gif", []byte{0x47, 0x49, 0x46, 0x38}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WritePixel(&buf, tt.path); err != nil {
				t.Fatal(err)
			}

			if buf.Len() == 0 {
				t.Error("pixel is empty")
			}

			if !bytes.HasPrefix(buf.Bytes(), tt.magic) {
				t.Errorf("unexpected magic bytes for %s", tt.path)
			}
		})
	}
}

func TestWriteOGPage(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteOGPage(&buf, "abc123.trap.example.com"); err != nil {
		t.Fatal(err)
	}

	html := buf.String()
	if !strings.Contains(html, "og:image") {
		t.Error("missing og:image meta tag")
	}

	if !strings.Contains(html, "abc123.trap.example.com") {
		t.Error("missing host in og:image URL")
	}
}
