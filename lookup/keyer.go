package lookup

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

func HashChunkTokens(tokens []TokenID) ChunkKey {
	encoded := make([]byte, 4+len(tokens)*4)
	binary.BigEndian.PutUint32(encoded[:4], uint32(len(tokens)))

	offset := 4
	for _, token := range tokens {
		binary.BigEndian.PutUint32(encoded[offset:offset+4], uint32(token))
		offset += 4
	}

	sum := sha256.Sum256(encoded)
	return ChunkKey(hex.EncodeToString(sum[:]))
}
