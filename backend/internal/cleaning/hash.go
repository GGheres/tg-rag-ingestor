package cleaning

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/fnv"
)

func ContentHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func SimhashPlaceholder(input string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(input))
	v := h.Sum64()
	return toHex(v)
}

func toHex(v uint64) string {
	const hexAlphabet = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hexAlphabet[v&0xF]
		v >>= 4
	}
	return string(out)
}
