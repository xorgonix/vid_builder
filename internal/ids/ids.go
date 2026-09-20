// Package ids generates sortable UUIDv7-compatible lowercase identifiers (spec §6.4).
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

var counter atomic.Uint64

// New returns a lowercase UUIDv7-style identifier: 48-bit unix-millisecond
// timestamp, version 7, random entropy. Lexicographic order == creation order.
func New() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	randA := counter.Add(1) & 0x0fff
	b[6] = 0x70 | byte(randA>>8) // version 7
	b[7] = byte(randA)
	var entropy [8]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic("ids: crypto/rand unavailable: " + err.Error())
	}
	copy(b[8:], entropy[:])
	// variant 10xx
	b[8] = (b[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}
