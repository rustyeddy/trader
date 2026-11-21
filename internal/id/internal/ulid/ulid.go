// Package ulid implements the exact 128-bit representation mechanics behind
// Trader-owned identifiers: packing a 48-bit millisecond timestamp and
// 80 bits of entropy into 16 bytes, and converting between that
// representation and its canonical 26-character Crockford Base32 text form
// (https://github.com/ulid/spec).
//
// This package knows nothing about Trader's identifier kinds, generation
// policy, or monotonic sequencing; it knows only how to pack, encode, and
// decode the 128 bits. The public id package owns everything semantic —
// which kind an identifier belongs to, how it is generated, and what the
// zero value means — the same division of responsibility num/internal/fixed
// has with num.
package ulid

import (
	"encoding/binary"
	"fmt"
	"time"
)

// EncodedLen is the length of a ULID's canonical text encoding: 128 bits at
// 5 bits per Crockford Base32 character, rounded up to 26.
const EncodedLen = 26

// maxTimestamp is the largest value New accepts: the largest 48-bit
// unsigned integer, corresponding to roughly the year 10889.
const maxTimestamp = (1 << 48) - 1

// New packs a 48-bit millisecond-since-Unix-epoch timestamp and 80 bits of
// entropy into a 128-bit value: the timestamp occupies the first 6 bytes,
// most significant byte first, and entropy fills the remaining 10.
func New(millis int64, entropy [10]byte) ([16]byte, error) {
	if millis < 0 || millis > maxTimestamp {
		return [16]byte{}, fmt.Errorf("ulid: timestamp %d milliseconds out of 48-bit range", millis)
	}

	var v [16]byte
	v[0] = byte(millis >> 40)
	v[1] = byte(millis >> 32)
	v[2] = byte(millis >> 24)
	v[3] = byte(millis >> 16)
	v[4] = byte(millis >> 8)
	v[5] = byte(millis)
	copy(v[6:], entropy[:])
	return v, nil
}

// Timestamp returns the UTC instant encoded in v's first 6 bytes.
func Timestamp(v [16]byte) time.Time {
	millis := int64(v[0])<<40 | int64(v[1])<<32 | int64(v[2])<<24 |
		int64(v[3])<<16 | int64(v[4])<<8 | int64(v[5])
	return time.UnixMilli(millis).UTC()
}

// Entropy returns v's last 10 bytes: the 80-bit component that is not the
// timestamp.
func Entropy(v [16]byte) [10]byte {
	var e [10]byte
	copy(e[:], v[6:])
	return e
}

// IncrementEntropy returns e+1, treating e as an unsigned 80-bit big-endian
// integer, and reports whether the increment overflowed — e was already
// the maximum representable value, 2^80-1. This is what lets a generator
// produce strictly increasing values for multiple identifiers created
// within the same millisecond, per the ULID spec's monotonic generation
// guidance, without drawing fresh entropy that could sort earlier than a
// sibling created moments before it.
func IncrementEntropy(e [10]byte) (next [10]byte, overflowed bool) {
	next = e
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			return next, false
		}
	}
	return next, true
}

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// reverse maps an alphabet byte to its 5-bit value, or 0xFF for a byte that
// is not in the alphabet. Crockford Base32 is decoded strictly here: no
// case-folding and no typo-tolerant substitution (Crockford's own spec
// permits treating "O" as "0" and "I"/"L" as "1", but this package rejects
// rather than repairs malformed input, matching num's exact-parsing
// policy elsewhere in this module).
var reverse [256]byte

func init() {
	for i := range reverse {
		reverse[i] = 0xFF
	}
	for i := range len(alphabet) {
		reverse[alphabet[i]] = byte(i)
	}
}

// Encode returns v's canonical 26-character Crockford Base32 encoding.
func Encode(v [16]byte) string {
	hi := binary.BigEndian.Uint64(v[0:8])
	lo := binary.BigEndian.Uint64(v[8:16])

	var out [EncodedLen]byte
	out[0] = alphabet[(hi>>61)&0x1F]
	out[1] = alphabet[(hi>>56)&0x1F]
	out[2] = alphabet[(hi>>51)&0x1F]
	out[3] = alphabet[(hi>>46)&0x1F]
	out[4] = alphabet[(hi>>41)&0x1F]
	out[5] = alphabet[(hi>>36)&0x1F]
	out[6] = alphabet[(hi>>31)&0x1F]
	out[7] = alphabet[(hi>>26)&0x1F]
	out[8] = alphabet[(hi>>21)&0x1F]
	out[9] = alphabet[(hi>>16)&0x1F]
	out[10] = alphabet[(hi>>11)&0x1F]
	out[11] = alphabet[(hi>>6)&0x1F]
	out[12] = alphabet[(hi>>1)&0x1F]
	out[13] = alphabet[((hi&0x1)<<4)|((lo>>60)&0x0F)]
	out[14] = alphabet[(lo>>55)&0x1F]
	out[15] = alphabet[(lo>>50)&0x1F]
	out[16] = alphabet[(lo>>45)&0x1F]
	out[17] = alphabet[(lo>>40)&0x1F]
	out[18] = alphabet[(lo>>35)&0x1F]
	out[19] = alphabet[(lo>>30)&0x1F]
	out[20] = alphabet[(lo>>25)&0x1F]
	out[21] = alphabet[(lo>>20)&0x1F]
	out[22] = alphabet[(lo>>15)&0x1F]
	out[23] = alphabet[(lo>>10)&0x1F]
	out[24] = alphabet[(lo>>5)&0x1F]
	out[25] = alphabet[lo&0x1F]
	return string(out[:])
}

// Decode parses s, which must be exactly EncodedLen characters from the
// Crockford Base32 alphabet, back into its 128-bit value. Decode rejects
// the wrong length, any character outside the alphabet, and a first
// character whose value would require more than the 3 significant bits
// available at the top of a 128-bit value (128 is not a multiple of 5, so
// the 26-character encoding has 2 unused leading bits; a first character
// value of 8 or more would need one of those bits, meaning the input does
// not represent 128 bits at all).
func Decode(s string) ([16]byte, error) {
	if len(s) != EncodedLen {
		return [16]byte{}, fmt.Errorf("ulid: wrong length: got %d characters, want %d", len(s), EncodedLen)
	}

	var v [EncodedLen]byte
	for i := range EncodedLen {
		b := reverse[s[i]]
		if b == 0xFF {
			return [16]byte{}, fmt.Errorf("ulid: invalid character %q at position %d", s[i], i)
		}
		v[i] = b
	}
	if v[0] > 7 {
		return [16]byte{}, fmt.Errorf("ulid: first character out of range: would overflow 128 bits")
	}

	hi := uint64(v[0])<<61 |
		uint64(v[1])<<56 | uint64(v[2])<<51 | uint64(v[3])<<46 |
		uint64(v[4])<<41 | uint64(v[5])<<36 | uint64(v[6])<<31 |
		uint64(v[7])<<26 | uint64(v[8])<<21 | uint64(v[9])<<16 |
		uint64(v[10])<<11 | uint64(v[11])<<6 | uint64(v[12])<<1 |
		uint64(v[13])>>4

	lo := uint64(v[13]&0x0F)<<60 |
		uint64(v[14])<<55 | uint64(v[15])<<50 | uint64(v[16])<<45 |
		uint64(v[17])<<40 | uint64(v[18])<<35 | uint64(v[19])<<30 |
		uint64(v[20])<<25 | uint64(v[21])<<20 | uint64(v[22])<<15 |
		uint64(v[23])<<10 | uint64(v[24])<<5 | uint64(v[25])

	var out [16]byte
	binary.BigEndian.PutUint64(out[0:8], hi)
	binary.BigEndian.PutUint64(out[8:16], lo)
	return out, nil
}
