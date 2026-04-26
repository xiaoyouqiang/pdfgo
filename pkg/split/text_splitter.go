package split

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MarkdownPreservingSplitter 在切分文本时保护 Markdown 结构（表格、链接等）。
// 用于对已经分段的结果进行二级切分，确保表格和链接不被截断。
type MarkdownPreservingSplitter struct {
	chunkSize    int      // 每个块的最大字符数
	chunkOverlap int      // 块之间的重叠字符数（当前未使用）
	separators   []string // 切分分隔符列表，按优先级排序
}

// NewMarkdownPreservingSplitter 创建一个保护 Markdown 结构的文本切分器。
// 分隔符按优先级排序：段落分隔 > 换行 > 中文句号 > 英文句号 > 其他标点 > 空格
func NewMarkdownPreservingSplitter(chunkSize, chunkOverlap int) *MarkdownPreservingSplitter {
	if chunkSize < 50 {
		chunkSize = 50
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}

	return &MarkdownPreservingSplitter{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
		separators: []string{
			"\n\n", // 段落分隔（最优先）
			"\n",   // 换行
			"。",    // 中文句号
			". ",   // 英文句号加空格
			"！",    // 中文感叹号
			"！ ",   // 中文感叹号加空格
			"？",    // 中文问号
			"？ ",   // 中文问号加空格
			"; ",   // 英文分号
			"；",    // 中文分号
			" ",    // 空格（最后选择）
		},
	}
}

// SplitText 切分文本，同时保护 Markdown 表格和链接不被截断。
//
// 处理流程：
//  1. 如果文本不超过块大小，直接返回
//  2. 将表格和链接替换为占位符（保护）
//  3. 对保护后的文本进行切分
//  4. 将占位符恢复为原始 Markdown 内容
func (s *MarkdownPreservingSplitter) SplitText(text string) ([]string, error) {
	// 在保护之前获取原始长度
	originalLen := utf8.RuneCountInString(text)

	// 文本已经足够短，无需切分
	if originalLen <= s.chunkSize {
		return []string{text}, nil
	}

	// 保护表格和链接（替换为占位符）
	protected, tableMap, linkMap := s.protectMarkdown(text)

	// 切分保护后的文本
	chunks := s.splitProtectedText(protected, originalLen)

	// 恢复表格和链接
	chunks = s.restoreMarkdown(chunks, tableMap, linkMap)

	return chunks, nil
}

// protectMarkdown 将 Markdown 表格和链接提取并替换为占位符，防止切分时被截断。
//   - 表格：匹配完整的 Markdown 管道表格（表头 + 分隔行 + 数据行）
//   - 链接：匹配 [text](url) 和 ![alt](url) 格式
func (s *MarkdownPreservingSplitter) protectMarkdown(text string) (string, map[string]string, map[string]string) {
	tableMap := make(map[string]string)
	linkMap := make(map[string]string)

	// 保护表格：匹配完整的 Markdown 表格结构
	tableRegex := regexp.MustCompile(`(\|[^\n]+\|\n\|[-| ]+\|\n(?:\|[^\n]+\|\n?)+)`)
	text = tableRegex.ReplaceAllStringFunc(text, func(match string) string {
		placeholder := fmt.Sprintf("<<TABLE_%d>>", len(tableMap))
		tableMap[placeholder] = match
		return placeholder
	})

	// 保护链接和图片：匹配 [text](url) 和 ![alt](url)
	linkRegex := regexp.MustCompile(`!?\[([^\]]*)\]\(([^\)]+)\)`)
	text = linkRegex.ReplaceAllStringFunc(text, func(match string) string {
		placeholder := fmt.Sprintf("<<LINK_%d>>", len(linkMap))
		linkMap[placeholder] = match
		return placeholder
	})

	return text, tableMap, linkMap
}

// restoreMarkdown 将占位符恢复为原始的 Markdown 表格和链接
func (s *MarkdownPreservingSplitter) restoreMarkdown(chunks []string, tableMap, linkMap map[string]string) []string {
	result := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		for placeholder, table := range tableMap {
			chunk = strings.ReplaceAll(chunk, placeholder, table)
		}
		for placeholder, link := range linkMap {
			chunk = strings.ReplaceAll(chunk, placeholder, link)
		}
		result = append(result, chunk)
	}
	return result
}

// splitProtectedText 对保护后的文本进行切分。
// originalLen 是保护前原始文本的长度，用于确定需要切分成多少块。
func (s *MarkdownPreservingSplitter) splitProtectedText(text string, originalLen int) []string {
	// 文本已经足够短
	if originalLen <= s.chunkSize {
		return []string{text}
	}

	var chunks []string
	remaining := text

	// 计算最少需要切分的块数
	minChunks := (originalLen + s.chunkSize - 1) / s.chunkSize

	for len(chunks) < minChunks {
		chunk, rest := s.findChunk(remaining)
		if chunk == "" {
			// 回退：直接取前 chunkSize 个字符
			chunk = runeSlice(remaining, 0, s.chunkSize)
			rest = runeSliceFrom(remaining, s.chunkSize)
		}

		chunks = append(chunks, chunk)
		remaining = rest

		// 安全检查：剩余为空或未变化时停止
		if remaining == "" || (len(chunks) > 0 && rest == chunks[len(chunks)-1]) {
			break
		}
	}

	if remaining != "" {
		chunks = append(chunks, remaining)
	}

	return chunks
}

// runeSlice 按字符（非字节）截取字符串
func runeSlice(s string, start, end int) string {
	runes := []rune(s)
	if start >= len(runes) {
		return ""
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
}

// runeSliceFrom 从指定位置截取到末尾
func runeSliceFrom(s string, pos int) string {
	return runeSlice(s, pos, len([]rune(s)))
}

// findPlaceholderRanges 查找文本中所有占位符的范围
func (s *MarkdownPreservingSplitter) findPlaceholderRanges(text string) [][]int {
	var ranges [][]int
	textRunes := []rune(text)
	i := 0
	for i < len(textRunes)-1 {
		if textRunes[i] == '<' && textRunes[i+1] == '<' {
			start := i
			i += 2
			// 查找匹配的 >>
			for i < len(textRunes)-1 {
				if textRunes[i] == '>' && textRunes[i+1] == '>' {
					end := i + 2
					ranges = append(ranges, []int{start, end})
					i++
					break
				}
				i++
			}
		} else {
			i++
		}
	}
	return ranges
}

// isPositionInPlaceholder 检查位置是否在占位符范围内
func isPositionInPlaceholder(pos int, ranges [][]int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// findChunk 从文本中找到一个合适大小的块和剩余部分。
// 按优先级尝试各种分隔符，避免在占位符内部切分。
func (s *MarkdownPreservingSplitter) findChunk(text string) (chunk string, rest string) {
	textRunes := []rune(text)
	textLen := len(textRunes)

	// 文本已经足够短
	if textLen <= s.chunkSize {
		return text, ""
	}

	// 查找占位符范围，避免在占位符内部切分
	placeholderRanges := s.findPlaceholderRanges(text)

	searchEnd := s.chunkSize

	// 如果切分点落在占位符内部，调整到占位符末尾
	for _, r := range placeholderRanges {
		if searchEnd > r[0] && searchEnd < r[1] {
			searchEnd = r[1]
		}
	}

	// 按优先级尝试各种分隔符
	for _, sep := range s.separators {
		sepRunes := []rune(sep)
		sepLen := len(sepRunes)

		// 从 searchEnd 向前搜索到 searchEnd/2
		for i := searchEnd - sepLen; i >= searchEnd/2; i-- {
			// 跳过占位符内部的位置
			if isPositionInPlaceholder(i, placeholderRanges) {
				continue
			}
			if isPositionInPlaceholder(i+sepLen-1, placeholderRanges) {
				continue
			}
			// 检查分隔符是否匹配
			found := true
			for j := 0; j < sepLen && i+j < textLen; j++ {
				if textRunes[i+j] != sepRunes[j] {
					found = false
					break
				}
			}
			if found && i+sepLen <= textLen {
				chunk = string(textRunes[:i+sepLen])
				rest = string(textRunes[i+sepLen:])
				return chunk, rest
			}
		}
	}

	// 未找到分隔符，尝试在单词边界处切分
	chunkEnd := searchEnd
	for i := searchEnd - 1; i >= searchEnd/2 && i < textLen; i-- {
		if isPositionInPlaceholder(i, placeholderRanges) {
			continue
		}
		b := textRunes[i]
		if b == ' ' || b == '\n' {
			chunkEnd = i
			break
		}
	}

	if chunkEnd >= searchEnd/2 && chunkEnd < searchEnd && chunkEnd < textLen {
		return string(textRunes[:chunkEnd]), string(textRunes[chunkEnd:])
	}

	// 最终回退：直接在 chunkSize 处切分（确保不切在占位符内部）
	for _, r := range placeholderRanges {
		if searchEnd >= r[0] && searchEnd < r[1] {
			searchEnd = r[1]
			break
		}
	}

	if searchEnd > textLen {
		searchEnd = textLen
	}

	return string(textRunes[:searchEnd]), string(textRunes[searchEnd:])
}

// SplitLargeBlocks 对分段结果中超过 chunkSize 的内容块进行二级切分。
// 保持原始的 Title、ParentChain、Level 等元数据不变，仅切分 Content。
// 使用 MarkdownPreservingSplitter 保护表格和链接不被截断。
func SplitLargeBlocks(results []SplitResult, chunkSize, chunkOverlap int) []SplitResult {
	if len(results) == 0 {
		return results
	}

	splitter := NewMarkdownPreservingSplitter(chunkSize, chunkOverlap)

	var newResults []SplitResult
	for _, result := range results {
		// 使用字符数（非字节数）判断是否需要切分
		if utf8.RuneCountInString(result.Content) <= chunkSize {
			newResults = append(newResults, result)
			continue
		}

		// 切分大块内容
		chunks, _ := splitter.SplitText(result.Content)
		for _, chunk := range chunks {
			chunk = strings.TrimSpace(chunk)
			if chunk != "" {
				newResults = append(newResults, SplitResult{
					Title:       result.Title,
					Content:     chunk,
					Keywords:    result.Keywords,
					ParentChain: result.ParentChain,
					Level:       result.Level,
				})
			}
		}
	}

	return newResults
}
