// Package textsegments prepares deliberately selected text for a 500-byte
// translation API. It never silently drops words or truncates an input.
package textsegments

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxTextBytes = 3000
const RequestBytes = 460

// Clean removes control characters from clipboard/UIA text but preserves
// intentional paragraph boundaries. It never changes the words themselves.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || (unicode.IsPrint(r) && r != utf8.RuneError) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func Split(s string) ([]string, error) {
	s = Clean(s)
	if s == "" {
		return nil, errors.New("没有选中文本")
	}
	if len([]byte(s)) > MaxTextBytes {
		return nil, errors.New("选中内容超过 3000 字节，请缩短选区")
	}
	chunks := make([]string, 0, 8)
	for len(s) > 0 {
		if len(s) <= RequestBytes {
			chunks = append(chunks, s)
			break
		}
		cut, lastSpace, lastSentence := 0, 0, 0
		for i, r := range s {
			n := utf8.RuneLen(r)
			if i+n > RequestBytes {
				break
			}
			cut = i + n
			if unicode.IsSpace(r) {
				lastSpace = cut
			}
			if r == '.' || r == '!' || r == '?' || r == '。' || r == '！' || r == '？' || r == ';' || r == '；' || r == '\n' {
				lastSentence = cut
			}
		}
		if lastSentence >= RequestBytes/3 {
			cut = lastSentence
		} else if lastSpace >= RequestBytes/3 {
			cut = lastSpace
		}
		if cut < 1 {
			return nil, errors.New("无法分割选中内容")
		}
		chunk := strings.TrimSpace(s[:cut])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		s = strings.TrimSpace(s[cut:])
	}
	return chunks, nil
}
