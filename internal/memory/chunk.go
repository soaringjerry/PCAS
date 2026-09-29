package memory

// SplitText uses Unicode rune offsets so evidence can locate Chinese text.
// These are context chunks, not semantic claims or model-token boundaries.
func SplitText(text string, size, overlap int) []Chunk {
	if size <= 0 || overlap < 0 || overlap >= size {
		panic("invalid chunk configuration")
	}
	runes := []rune(text)
	chunks := make([]Chunk, 0)
	for start := 0; start < len(runes); start += size - overlap {
		end := min(start+size, len(runes))
		chunks = append(chunks, Chunk{Ordinal: len(chunks), StartRune: start, EndRune: end, Text: string(runes[start:end])})
		if end == len(runes) {
			break
		}
	}
	return chunks
}
