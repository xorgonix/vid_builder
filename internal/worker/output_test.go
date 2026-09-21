package worker

import "testing"

func TestOutputMediaClassification(t *testing.T) {
	cases := map[string][2]string{
		"vidbuild_00001_.png": {"image", "image/png"},
		"clip_00001_.webm":    {"video", "video/webm"},
		"clip_00001_.mp4":     {"video", "video/mp4"},
		"frame_00001_.webp":   {"image", "image/webp"},
		"frame_00001_.jpg":    {"image", "image/jpeg"},
		"frame_00001_.JPEG":   {"image", "image/jpeg"},
		"anim_00001_.gif":     {"image", "image/gif"},
		"unknown_00001_":      {"image", "image/png"},
	}
	for name, want := range cases {
		kind, mime := outputMedia(name)
		if kind != want[0] || mime != want[1] {
			t.Fatalf("outputMedia(%q) = (%q,%q), want (%q,%q)", name, kind, mime, want[0], want[1])
		}
	}
}
