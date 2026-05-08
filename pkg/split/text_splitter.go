// Package split 提供 Markdown 文本的智能分段功能。
//
// 本文件实现了 MarkdownPreservingSplitter，用于对已分段的结果进行二级切分。
// 核心特点：在切分时保护 Markdown 表格和链接不被截断。
//
// 处理流程：
//  1. 检测文本中的 Markdown 表格和链接，替换为短占位符（保护阶段）
//  2. 按分隔符优先级在合适的位置切分文本（切分阶段）
//  3. 将占位符恢复为原始 Markdown 内容（恢复阶段）
package split

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MarkdownPreservingSplitter 在切分文本时保护 Markdown 结构（表格、链接等）。
// 用于对已经分段的结果进行二级切分，确保表格和链接不被截断。
//
// 重叠（Overlap）机制：
//
//	相邻块之间共享 chunkOverlap 个字符的上下文，避免跨块语义断裂。
//	例如 chunkSize=1000, chunkOverlap=200 时：
//	  块1: 字符 [0, 1000)
//	  块2: 字符 [800, 1800)   ← 前 200 字符与块1 末尾重叠
//	  块3: 字符 [1600, 2600)  ← 前 200 字符与块2 末尾重叠
type MarkdownPreservingSplitter struct {
	chunkSize    int      // 每个块的最大字符数（rune 计数）
	chunkOverlap int      // 相邻块之间的重叠字符数（rune 计数），不超过 chunkSize/2
	separators   []string // 切分分隔符列表，按优先级从高到低排序
}

// NewMarkdownPreservingSplitter 创建一个保护 Markdown 结构的文本切分器。
//
// 参数：
//   - chunkSize: 每个块的最大字符数，小于 50 会被自动调整为 50
//   - chunkOverlap: 相邻块之间的重叠字符数，小于 0 调整为 0，超过 chunkSize/2 调整为 chunkSize/2
//
// 分隔符按优先级排序：段落分隔 > 换行 > 中文句号 > 英文句号 > 其他标点 > 空格
func NewMarkdownPreservingSplitter(chunkSize, chunkOverlap int) *MarkdownPreservingSplitter {
	if chunkSize < 50 {
		chunkSize = 50
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	// 限制 overlap 不超过 chunkSize 的一半，避免重叠区域过大导致切分效率低下
	if chunkOverlap > chunkSize/2 {
		chunkOverlap = chunkSize / 2
	}

	return &MarkdownPreservingSplitter{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
		// 分隔符按优先级排序，优先在自然断句处切分
		separators: []string{
			"\n\n", // 段落分隔（最优先，语义最完整）
			"\n",   // 换行符
			"。",    // 中文句号
			". ",   // 英文句号加空格
			"！",    // 中文感叹号
			"！ ",   // 中文感叹号加空格
			"？",    // 中文问号
			"？ ",   // 中文问号加空格
			"; ",   // 英文分号加空格
			"；",    // 中文分号
			//", ",   // 英文逗号（粒度太细，暂不启用）
			//"，",   // 中文逗号（粒度太细，暂不启用）
			" ", // 空格（最后选择，保证至少能切分）
		},
	}
}

// SplitText 切分文本，同时保护 Markdown 表格和链接不被截断。
//
// 处理流程：
//  1. 如果文本不超过块大小，直接返回
//  2. 将表格和链接替换为占位符（保护阶段）
//  3. 对保护后的文本进行切分
//  4. 将占位符恢复为原始 Markdown 内容
func (s *MarkdownPreservingSplitter) SplitText(text string) ([]string, error) {
	// 在保护之前获取原始文本的字符数，用于后续计算最少切分块数
	originalLen := utf8.RuneCountInString(text)

	// 文本已经足够短，无需切分
	if originalLen <= s.chunkSize {
		return []string{text}, nil
	}

	// 保护阶段：将表格和链接替换为短占位符，防止切分时被截断
	protected, tableMap, linkMap := s.protectMarkdown(text)

	// 切分阶段：对保护后的文本按分隔符优先级进行切分
	chunks := s.splitProtectedText(protected, originalLen)

	// 恢复阶段：将占位符替换回原始的表格和链接内容
	chunks = s.restoreMarkdown(chunks, tableMap, linkMap)

	return chunks, nil
}

// protectMarkdown 将 Markdown 表格和链接提取并替换为占位符，防止切分时被截断。
//
// 保护内容：
//   - 表格：匹配完整的 Markdown 管道表格（表头行 + 分隔行 + 数据行）
//   - 链接：匹配 [text](url) 和 ![alt](url) 格式
//
// 占位符格式：<<TABLE_0>>, <<TABLE_1>>, <<LINK_0>>, <<LINK_1>> ...
//
// 返回值：
//   - 替换后的文本
//   - 表格占位符映射表
//   - 链接占位符映射表
func (s *MarkdownPreservingSplitter) protectMarkdown(text string) (string, map[string]string, map[string]string) {
	tableMap := make(map[string]string)
	linkMap := make(map[string]string)

	// 保护表格：匹配完整的 Markdown 表格结构
	// 格式：| col1 | col2 | \n | --- | --- | \n | data | data |
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

// restoreMarkdown 将占位符恢复为原始的 Markdown 表格和链接内容。
// 遍历每个 chunk，逐一替换其中的表格和链接占位符。
func (s *MarkdownPreservingSplitter) restoreMarkdown(chunks []string, tableMap, linkMap map[string]string) []string {
	result := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		// 恢复表格占位符
		for placeholder, table := range tableMap {
			chunk = strings.ReplaceAll(chunk, placeholder, table)
		}
		// 恢复链接占位符
		for placeholder, link := range linkMap {
			chunk = strings.ReplaceAll(chunk, placeholder, link)
		}
		result = append(result, chunk)
	}
	return result
}

// splitProtectedText 对保护后的文本进行切分。
//
// 使用 originalLen（保护前的原始文本长度）来估算最少需要切分的块数，
// 因为保护后的文本中占位符比原始内容短，不能直接用保护后文本长度计算。
//
// 重叠机制：
//
//	每个块切出后，下一个块从当前块末尾 chunkOverlap 个字符处开始，
//	使相邻块之间有上下文重叠，避免跨块语义断裂。
//	重叠区域会自动避开占位符边界（不会在占位符中间开始）。
//
// 切分策略：
//  1. 计算最少需要的块数（ceil(原始长度 / (chunkSize - chunkOverlap))）
//  2. 循环调用 findChunk 找到每个切分点
//  3. 应用重叠：下一块 = 当前块末尾 overlap 个字符 + 剩余文本
//  4. 如果 findChunk 返回空（极端情况），回退到固定长度切分
//  5. 剩余内容作为最后一个块
func (s *MarkdownPreservingSplitter) splitProtectedText(text string, originalLen int) []string {
	// 文本已经足够短，无需切分
	if originalLen <= s.chunkSize {
		return []string{text}
	}

	var chunks []string
	remaining := text

	// 每个块实际覆盖的新内容量 = chunkSize - chunkOverlap（扣除与前一块重叠的部分）
	effectiveSize := s.chunkSize - s.chunkOverlap
	if effectiveSize <= 0 {
		effectiveSize = 1
	}
	// 向上取整计算最少块数
	minChunks := (originalLen + effectiveSize - 1) / effectiveSize
	// 用保护后文本的实际长度约束 minChunks 上限
	// 避免原始文本中大量表格/链接被替换为短占位符后，minChunks 远超实际需要的块数
	protectedLen := utf8.RuneCountInString(text)
	if protectedLen > 0 {
		maxPossibleChunks := (protectedLen + effectiveSize - 1) / effectiveSize
		if minChunks > maxPossibleChunks {
			minChunks = maxPossibleChunks
		}
	}

	for len(chunks) < minChunks && remaining != "" {
		chunk, rest := s.findChunk(remaining)
		if chunk == "" {
			// 回退策略：直接取前 chunkSize 个字符
			chunk = runeSlice(remaining, 0, s.chunkSize)
			rest = runeSliceFrom(remaining, s.chunkSize)
		}

		chunks = append(chunks, chunk)

		// 应用重叠逻辑：下一块从当前块末尾 overlap 个字符处开始
		if s.chunkOverlap > 0 && rest != "" {
			chunkRunes := []rune(chunk)
			// 只有当块长度大于 overlap 时才应用重叠，否则会退化为无进展
			if len(chunkRunes) > s.chunkOverlap {
				overlapText := s.extractOverlap(chunk)
				remaining = overlapText + rest
			} else {
				remaining = rest
			}
		} else {
			remaining = rest
		}

		// 安全检查：如果剩余内容为空，停止切分
		if remaining == "" {
			break
		}
	}

	// 将剩余内容作为最后一个块
	if remaining != "" {
		chunks = append(chunks, remaining)
	}

	return chunks
}

// extractOverlap 从块的末尾提取 chunkOverlap 个字符作为下一块的重叠前缀。
// 会自动调整起始位置，确保不在占位符（<<TABLE_N>> 或 <<LINK_N>>）内部截断：
// 如果截取起点落在占位符内部，向前移动到该占位符的起始位置。
func (s *MarkdownPreservingSplitter) extractOverlap(chunk string) string {
	chunkRunes := []rune(chunk)
	overlapStart := len(chunkRunes) - s.chunkOverlap
	if overlapStart <= 0 {
		return chunk
	}

	// 检查 overlapStart 是否落在占位符内部，如果是则向前调整到占位符起点
	placeholderRanges := s.findPlaceholderRanges(chunk)
	for _, r := range placeholderRanges {
		if overlapStart > r[0] && overlapStart < r[1] {
			overlapStart = r[0]
			break
		}
	}

	if overlapStart <= 0 {
		return chunk
	}

	return string(chunkRunes[overlapStart:])
}

// runeSlice 按字符（rune）截取字符串 [start, end)。
// 与直接使用字符串切片不同，此函数按字符边界截取，不会在多字节 UTF-8 字符中间断裂。
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

// runeSliceFrom 从指定位置截取到字符串末尾。
func runeSliceFrom(s string, pos int) string {
	return runeSlice(s, pos, len([]rune(s)))
}

// findPlaceholderRanges 查找文本中所有占位符的 rune 范围。
//
// 占位符格式为 <<TABLE_N>> 或 <<LINK_N>>。
// 返回值是 [start, end) 形式的范围列表，start 和 end 都是 rune 索引。
//
// 用于后续切分时避免在占位符内部进行分割。
func (s *MarkdownPreservingSplitter) findPlaceholderRanges(text string) [][]int {
	var ranges [][]int
	textRunes := []rune(text)
	i := 0
	for i < len(textRunes)-1 {
		// 检测占位符开始标记 "<<"
		if textRunes[i] == '<' && textRunes[i+1] == '<' {
			start := i
			i += 2
			// 查找占位符结束标记 ">>"
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

// isPositionInPlaceholder 检查给定的 rune 位置是否落在某个占位符范围内。
// 用于切分时跳过占位符内部的位置，避免破坏占位符字符串。
func isPositionInPlaceholder(pos int, ranges [][]int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// findChunk 从文本中找到一个合适大小的块和剩余部分。
//
// 切分策略（按优先级）：
//  1. 在 chunkSize 附近向后搜索（到 chunkSize/2），查找匹配的分隔符
//  2. 跳过占位符内部的位置，确保不破坏表格/链接占位符
//  3. 如果找不到合适的分隔符，尝试在空格或换行处切分（单词边界）
//  4. 最终回退：直接在 chunkSize 处切分（确保不切在占位符内部）
//
// 返回值：
//   - chunk: 切分出的文本块
//   - rest: 剩余的文本
func (s *MarkdownPreservingSplitter) findChunk(text string) (chunk string, rest string) {
	textRunes := []rune(text)
	textLen := len(textRunes)

	// 文本已经足够短，直接返回全部
	if textLen <= s.chunkSize {
		return text, ""
	}

	// 查找占位符范围，避免在占位符内部切分
	placeholderRanges := s.findPlaceholderRanges(text)

	searchEnd := s.chunkSize

	// 如果切分点 searchEnd 落在占位符内部，调整到占位符末尾
	for _, r := range placeholderRanges {
		if searchEnd > r[0] && searchEnd < r[1] {
			searchEnd = r[1]
		}
	}

	// 策略一：按分隔符优先级，从 searchEnd 向前搜索到 searchEnd/2
	// 寻找最佳的自然断句点
	for _, sep := range s.separators {
		sepRunes := []rune(sep)
		sepLen := len(sepRunes)

		// 从 searchEnd 向前搜索到 searchEnd/2，寻找分隔符
		for i := searchEnd - sepLen; i >= searchEnd/2; i-- {
			// 跳过占位符内部的位置
			if isPositionInPlaceholder(i, placeholderRanges) {
				continue
			}
			// 检查分隔符的末尾字符是否也在占位符内部
			if isPositionInPlaceholder(i+sepLen-1, placeholderRanges) {
				continue
			}
			// 逐字符比较，检查分隔符是否匹配
			found := true
			for j := 0; j < sepLen && i+j < textLen; j++ {
				if textRunes[i+j] != sepRunes[j] {
					found = false
					break
				}
			}
			if found && i+sepLen <= textLen {
				// 找到分隔符，在分隔符之后切分
				chunk = string(textRunes[:i+sepLen])
				rest = string(textRunes[i+sepLen:])
				return chunk, rest
			}
		}
	}

	// 策略二：未找到分隔符，尝试在单词边界（空格或换行）处切分
	chunkEnd := searchEnd
	for i := searchEnd - 1; i >= searchEnd/2 && i < textLen; i-- {
		// 跳过占位符内部的位置
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

	// 策略三：最终回退，直接在 searchEnd 处切分
	// 先确保不在占位符内部切分
	for _, r := range placeholderRanges {
		if searchEnd >= r[0] && searchEnd < r[1] {
			searchEnd = r[1]
			break
		}
	}

	// 确保不超过文本长度
	if searchEnd > textLen {
		searchEnd = textLen
	}

	return string(textRunes[:searchEnd]), string(textRunes[searchEnd:])
}

// SplitLargeBlocks 对分段结果中超过 chunkSize 的内容块进行二级切分。
//
// 保持原始的 Title、ParentChain、Level、OriginLevel 等元数据不变，仅切分 Content 字段。
// 使用 MarkdownPreservingSplitter 保护表格和链接不被截断。
//
// 参数：
//   - results: 一级分段结果列表
//   - chunkSize: 每个块的最大字符数（rune 计数）
//   - chunkOverlap: 相邻块之间的重叠字符数（rune 计数），超过 chunkSize/2 自动调整
//   - isTocTitleResult: 是否为"有章节目录组织形式"的内容。
//     当为 true 时，会设置 ParentIndex 字段，用于标记子块与父块的关联关系
func SplitLargeBlocks(results []SplitResult, chunkSize, chunkOverlap int, isTocTitleResult bool) []SplitResult {
	if len(results) == 0 {
		return results
	}

	splitter := NewMarkdownPreservingSplitter(chunkSize, chunkOverlap)
	var newResults []SplitResult
	for index, result := range results {
		// 使用字符数（rune 计数）判断是否需要切分
		if utf8.RuneCountInString(result.Content) <= chunkSize {
			// 有章节目录的内容组织形式，需要关联父块索引
			if isTocTitleResult {
				result.ParentIndex = index
			}
			newResults = append(newResults, result)
			continue
		}

		// 对超长内容块进行二级切分
		chunks, _ := splitter.SplitText(result.Content)
		for _, chunk := range chunks {
			chunk = strings.TrimSpace(chunk)
			if chunk != "" {
				// 有章节目录的内容组织形式，需要关联父块索引
				if isTocTitleResult {
					result.ParentIndex = index
				}
				newResults = append(newResults, SplitResult{
					Title:       result.Title,
					Content:     chunk,
					Keywords:    result.Keywords,
					ParentChain: result.ParentChain,
					Level:       result.Level,
					ParentIndex: result.ParentIndex,
				})
			}
		}
	}

	return newResults
}
