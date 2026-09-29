package ports

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"sync"
	"time"
)

var (
	idMu     sync.Mutex
	idLastMs int64
	idSeq    uint16
)

// NewID returns an RFC 9562 UUIDv7 string: time-ordered, so primary-key inserts stay index-friendly.
// Within one millisecond a 12-bit counter keeps IDs from one process strictly increasing.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error()) // unrecoverable: no secure randomness
	}
	idMu.Lock()
	ms := time.Now().UnixMilli()
	if ms <= idLastMs {
		ms = idLastMs
		idSeq++
		if idSeq > 0x0fff { // counter exhausted: borrow the next millisecond
			ms++
			idSeq = 0
		}
	} else {
		idSeq = binary.BigEndian.Uint16(b[6:8]) & 0x03ff // random start, headroom for increments
	}
	idLastMs = ms
	seq := idSeq
	idMu.Unlock()

	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70 | byte(seq>>8)&0x0f
	b[7] = byte(seq)
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
