package lookup

import "fmt"

// BuildChunks creates lookup chunks from the full-token prefix only.
func BuildChunks(tokens []TokenID, chunkSize int) ([]Chunk, int, error) {
	if chunkSize <= 0 {
		return nil, 0, fmt.Errorf("lookup: chunk size must be greater than 0")
	}

	fullChunkCount := len(tokens) / chunkSize
	chunks := make([]Chunk, 0, fullChunkCount)
	for index := 0; index < fullChunkCount; index++ {
		start := index * chunkSize
		end := start + chunkSize
		chunks = append(chunks, Chunk{
			Index: index,
			Start: start,
			End:   end,
			Key:   HashChunkTokens(tokens[start:end]),
		})
	}

	return chunks, fullChunkCount * chunkSize, nil
}
