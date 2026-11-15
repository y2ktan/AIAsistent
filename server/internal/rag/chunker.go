package rag

import (
	"strings"
	"unicode/utf8"
)

type Chunk struct {
	Index      int
	Content    string
	TokenCount int
}

func ChunkText(text string, maxChars, overlapChars int) []Chunk {
	if maxChars <= 0 {
		maxChars = 2400
	}
	if overlapChars < 0 || overlapChars >= maxChars {
		overlapChars = maxChars / 6
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	runes := []rune(trimmed)
	total := len(runes)
	if total <= maxChars {
		return []Chunk{{
			Index:      0,
			Content:    trimmed,
			TokenCount: estimateTokens(trimmed),
		}}
	}

	var chunks []Chunk
	start := 0
	idx := 0
	for start < total {
		end := start + maxChars
		if end >= total {
			end = total
		} else {
			// Prefer breaking on newline or space near the end
			searchStart := end - maxChars/5
			if searchStart < start {
				searchStart = start
			}
			bestBreak := -1
			for i := end - 1; i >= searchStart; i-- {
				if runes[i] == '\n' {
					bestBreak = i + 1
					break
				}
				if runes[i] == ' ' && bestBreak == -1 {
					bestBreak = i + 1
				}
			}
			if bestBreak > start {
				end = bestBreak
			}
		}

		piece := strings.TrimSpace(string(runes[start:end]))
		if piece != "" {
			chunks = append(chunks, Chunk{
				Index:      idx,
				Content:    piece,
				TokenCount: estimateTokens(piece),
			})
			idx++
		}

		if end >= total {
			break
		}
		nextStart := end - overlapChars
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}
	return chunks
}

func estimateTokens(s string) int {
	n := utf8.RuneCountInString(s) / 4
	if n < 1 && len(s) > 0 {
		return 1
	}
	return n
}
