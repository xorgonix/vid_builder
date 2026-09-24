package worker

import "fmt"

// isImageHeader reports whether the leading bytes identify a PNG or JPEG image
// (R1.8). A filename extension alone is not proof.
func isImageHeader(head []byte) bool {
	if len(head) >= 8 && head[0] == 0x89 && head[1] == 'P' && head[2] == 'N' && head[3] == 'G' {
		return true
	}
	if len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF {
		return true
	}
	return false
}

var _ = fmt.Sprintf