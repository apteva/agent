package memory

import (
	"strings"
	"unicode"
)

// Chunk represents a piece of a document
type Chunk struct {
	Content  string
	Index    int
	StartPos int
	EndPos   int
}

// DocumentChunker splits documents into overlapping chunks for memory storage
type DocumentChunker struct {
	MaxChunkSize int // Max characters per chunk
	ChunkOverlap int // Overlap between chunks
	MinChunkSize int // Minimum chunk size (skip smaller)
}

// NewDocumentChunker creates a new chunker with the given settings
func NewDocumentChunker(maxSize, overlap int) *DocumentChunker {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if overlap <= 0 {
		overlap = 200
	}
	if overlap >= maxSize {
		overlap = maxSize / 5
	}

	return &DocumentChunker{
		MaxChunkSize: maxSize,
		ChunkOverlap: overlap,
		MinChunkSize: 50,
	}
}

// ChunkText splits text into overlapping chunks
// Uses paragraph-aware splitting when possible
func (dc *DocumentChunker) ChunkText(content string) []Chunk {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	// If content is smaller than max chunk size, return as single chunk
	if len(content) <= dc.MaxChunkSize {
		return []Chunk{{
			Content:  content,
			Index:    0,
			StartPos: 0,
			EndPos:   len(content),
		}}
	}

	// Split into paragraphs first
	paragraphs := splitIntoParagraphs(content)

	var chunks []Chunk
	var currentChunk strings.Builder
	var chunkStart int
	currentPos := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			currentPos += 2 // Account for paragraph break
			continue
		}

		// If adding this paragraph would exceed max size
		if currentChunk.Len() > 0 && currentChunk.Len()+len(para)+1 > dc.MaxChunkSize {
			// Save current chunk
			chunkContent := strings.TrimSpace(currentChunk.String())
			if len(chunkContent) >= dc.MinChunkSize {
				chunks = append(chunks, Chunk{
					Content:  chunkContent,
					Index:    len(chunks),
					StartPos: chunkStart,
					EndPos:   currentPos,
				})
			}

			// Start new chunk with overlap
			currentChunk.Reset()
			overlapText := getOverlapText(chunkContent, dc.ChunkOverlap)
			if overlapText != "" {
				currentChunk.WriteString(overlapText)
				currentChunk.WriteString("\n\n")
			}
			chunkStart = currentPos - len(overlapText)
		}

		// If single paragraph is larger than max size, split it
		if len(para) > dc.MaxChunkSize {
			// First, save any accumulated content
			if currentChunk.Len() >= dc.MinChunkSize {
				chunkContent := strings.TrimSpace(currentChunk.String())
				chunks = append(chunks, Chunk{
					Content:  chunkContent,
					Index:    len(chunks),
					StartPos: chunkStart,
					EndPos:   currentPos,
				})
				currentChunk.Reset()
			}

			// Split the large paragraph by sentences
			sentenceChunks := dc.splitLargeParagraph(para, currentPos)
			for _, sc := range sentenceChunks {
				sc.Index = len(chunks)
				chunks = append(chunks, sc)
			}
			chunkStart = currentPos + len(para)
		} else {
			// Add paragraph to current chunk
			if currentChunk.Len() > 0 {
				currentChunk.WriteString("\n\n")
			}
			currentChunk.WriteString(para)
		}

		currentPos += len(para) + 2 // +2 for paragraph break
	}

	// Don't forget the last chunk
	if currentChunk.Len() >= dc.MinChunkSize {
		chunkContent := strings.TrimSpace(currentChunk.String())
		chunks = append(chunks, Chunk{
			Content:  chunkContent,
			Index:    len(chunks),
			StartPos: chunkStart,
			EndPos:   currentPos,
		})
	}

	// Normalize whitespace in all chunks - collapse multiple newlines to double
	for i := range chunks {
		chunks[i].Content = normalizeChunkWhitespace(chunks[i].Content)
	}

	return chunks
}

// splitLargeParagraph splits a paragraph that exceeds max size into sentence-based chunks
func (dc *DocumentChunker) splitLargeParagraph(para string, basePos int) []Chunk {
	sentences := splitIntoSentences(para)

	var chunks []Chunk
	var currentChunk strings.Builder
	var chunkStart int = basePos
	currentPos := basePos

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		// If adding this sentence would exceed max size
		if currentChunk.Len() > 0 && currentChunk.Len()+len(sentence)+1 > dc.MaxChunkSize {
			// Save current chunk
			chunkContent := strings.TrimSpace(currentChunk.String())
			if len(chunkContent) >= dc.MinChunkSize {
				chunks = append(chunks, Chunk{
					Content:  chunkContent,
					Index:    len(chunks), // Will be adjusted by caller
					StartPos: chunkStart,
					EndPos:   currentPos,
				})
			}

			// Start new chunk with overlap
			currentChunk.Reset()
			overlapText := getOverlapText(chunkContent, dc.ChunkOverlap)
			if overlapText != "" {
				currentChunk.WriteString(overlapText)
				currentChunk.WriteString(" ")
			}
			chunkStart = currentPos - len(overlapText)
		}

		// Add sentence to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(sentence)
		currentPos += len(sentence) + 1
	}

	// Last chunk
	if currentChunk.Len() >= dc.MinChunkSize {
		chunkContent := strings.TrimSpace(currentChunk.String())
		chunks = append(chunks, Chunk{
			Content:  chunkContent,
			Index:    len(chunks),
			StartPos: chunkStart,
			EndPos:   currentPos,
		})
	}

	return chunks
}

// splitIntoParagraphs splits text on double newlines, or single newlines if no double newlines exist
func splitIntoParagraphs(text string) []string {
	// Normalize line endings
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// First try splitting on double newlines (paragraphs)
	paragraphs := strings.Split(text, "\n\n")

	// If we only got one paragraph and it's large, the file likely uses single newlines
	// Split on single newlines instead for line-by-line content
	if len(paragraphs) == 1 && len(paragraphs[0]) > 1000 {
		lines := strings.Split(text, "\n")
		var result []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				result = append(result, line)
			}
		}
		if len(result) > 1 {
			return result
		}
	}

	// Filter empty paragraphs
	var result []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result
}

// splitIntoSentences splits text into sentences
func splitIntoSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)

		// Check for sentence-ending punctuation
		if r == '.' || r == '!' || r == '?' {
			// Look ahead to see if this is really a sentence end
			if i+1 < len(runes) {
				next := runes[i+1]
				// If followed by space and uppercase letter, it's a sentence end
				if unicode.IsSpace(next) {
					if i+2 < len(runes) && unicode.IsUpper(runes[i+2]) {
						sentences = append(sentences, strings.TrimSpace(current.String()))
						current.Reset()
						i++ // Skip the space
						continue
					}
				}
				// If followed by quote then space, also sentence end
				if next == '"' || next == '\'' || next == ')' {
					sentences = append(sentences, strings.TrimSpace(current.String()))
					current.Reset()
					continue
				}
			} else {
				// End of text
				sentences = append(sentences, strings.TrimSpace(current.String()))
				current.Reset()
			}
		}
	}

	// Don't forget remaining text
	if current.Len() > 0 {
		remaining := strings.TrimSpace(current.String())
		if remaining != "" {
			sentences = append(sentences, remaining)
		}
	}

	return sentences
}

// getOverlapText returns the last N characters of text for overlap
// Tries to break at word boundaries
func getOverlapText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	// Get last maxLen characters
	overlap := text[len(text)-maxLen:]

	// Try to find a word boundary
	spaceIdx := strings.Index(overlap, " ")
	if spaceIdx > 0 && spaceIdx < len(overlap)/2 {
		overlap = overlap[spaceIdx+1:]
	}

	return strings.TrimSpace(overlap)
}

// normalizeChunkWhitespace collapses multiple consecutive newlines into double newlines
// This keeps paragraph structure but removes excessive whitespace
func normalizeChunkWhitespace(text string) string {
	// Replace 3+ consecutive newlines with just 2
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(text)
}
