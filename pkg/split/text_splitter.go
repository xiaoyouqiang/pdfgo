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
//	重叠带语义边界门槛：仅当块尾 chunkOverlap 窗口内存在语义边界
//	（段落 > 换行 > 句末标点）时，取边界之后的尾巴作为下一块的重叠前缀，
//	重叠起点必落在句子/段落边界之后；找不到语义边界则不产生重叠。
//	内容不超过 chunkSize 的块不会被切分，自然无重叠。
type MarkdownPreservingSplitter struct {
	chunkSize           int      // 每个块的最大字符数（rune 计数）
	chunkOverlap        int      // 相邻块之间的重叠字符数上限（rune 计数），不超过 chunkSize/2
	tableSplitThreshold int      // 表格原子保护单元的最大容忍字符数（rune 计数），超过则预切为多张带表头子表；0 值回退为 chunkSize
	separators          []string // 切分分隔符列表，按优先级从高到低排序
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
		chunkSize:           chunkSize,
		chunkOverlap:        chunkOverlap,
		tableSplitThreshold: chunkSize, // 默认与块大小一致：任何原子保护单元都不应超过块大小
		// 分隔符按优先级排序，优先在自然断句处切分
		separators: []string{
			"\n\n", // 段落分隔（最优先，语义最完整）
			"\n",   // 换行符
			"。",    // 中文句号
			"！",    // 中文感叹号
			"！",    // 中文感叹号加空格
			"？",    // 中文问号
			"？",    // 中文问号加空格
			";",    // 英文分号加空格
			"；",    // 中文分号
			".",    // 英文句号加空格
			//", ",   // 英文逗号（粒度太细，暂不启用）
			//"，",   // 中文逗号（粒度太细，暂不启用）
			" ", // 空格（最后选择，保证至少能切分）
		},
	}
}

// SetTableSplitThreshold 设置表格原子保护单元的最大容忍字符数（rune 计数）：
// 超过该值的表格会在保护阶段按数据行预切为多张完整子表（每张保留表头+分隔行）。
// n ≤ 0 时回退为默认值 chunkSize。
func (s *MarkdownPreservingSplitter) SetTableSplitThreshold(n int) {
	if n <= 0 {
		n = s.chunkSize
	}
	s.tableSplitThreshold = n
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

	// 切分阶段
	var chunks []string
	if utf8.RuneCountInString(protected) <= s.chunkSize {
		// 保护后不超限但原文超限：超限部分都在占位符里（表格为主的内容），
		// 按分隔符切分永远不会触发。走按行有效长度装箱的兜底路径，在占位符
		// 边界处分开，恢复后每个块的真实长度尽量 ≤ chunkSize。
		chunks = s.packProtectedLines(protected, tableMap, linkMap)
	} else {
		chunks = s.splitProtectedText(protected, originalLen)
	}

	// 恢复阶段：将占位符替换回原始的表格和链接内容
	chunks = s.restoreMarkdown(chunks, tableMap, linkMap)

	return chunks, nil
}

// packProtectedLines 处理「原文超过 chunkSize 但占位符保护后不超限」的文本。
//
// 表格等重型内容被替换成短占位符后，按分隔符切分的路径感知不到超限。
// 这里按行贪心装箱：行是原子单位（不切进行内、占位符不可破坏），
// 行长按有效长度计——占位符按其恢复后的字符数计——
// 使恢复后每个块的真实长度尽量 ≤ chunkSize。单行有效长度超限时独立成块。
func (s *MarkdownPreservingSplitter) packProtectedLines(protected string, tableMap, linkMap map[string]string) []string {
	// 单行有效长度：自身 rune 数 + 行内占位符恢复后多出的部分
	lineEffective := func(line string) int {
		n := utf8.RuneCountInString(line)
		lineRunes := []rune(line)
		for _, rng := range s.findPlaceholderRanges(line) {
			ph := string(lineRunes[rng[0]:rng[1]])
			if restored, ok := tableMap[ph]; ok {
				n += utf8.RuneCountInString(restored) - (rng[1] - rng[0])
			} else if restored, ok := linkMap[ph]; ok {
				n += utf8.RuneCountInString(restored) - (rng[1] - rng[0])
			}
		}
		return n
	}

	var chunks []string
	var cur []string
	curLen := 0
	flush := func() {
		chunk := strings.TrimSpace(strings.Join(cur, "\n"))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		cur = nil
		curLen = 0
	}
	for _, line := range strings.Split(protected, "\n") {
		eff := lineEffective(line)
		if curLen > 0 && curLen+1+eff > s.chunkSize {
			flush()
		}
		if curLen > 0 {
			curLen++ // 行间换行
		}
		cur = append(cur, line)
		curLen += eff
	}
	flush()
	return chunks
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
	// 超过 tableSplitThreshold 的表格先按数据行预切为多张完整子表（每张保留表头+分隔行），
	// 每张子表各占一个占位符——保证不产生超过阈值的原子保护单元。
	tableRegex := regexp.MustCompile(`(\|[^\n]+\|\n\|[-| ]+\|\n(?:\|[^\n]+\|\n?)+)`)
	text = tableRegex.ReplaceAllStringFunc(text, func(match string) string {
		fragments := s.splitOversizedTable(match)
		if fragments == nil {
			placeholder := fmt.Sprintf("<<TABLE_%d>>", len(tableMap))
			tableMap[placeholder] = match
			return placeholder
		}
		placeholders := make([]string, 0, len(fragments))
		for _, frag := range fragments {
			placeholder := fmt.Sprintf("<<TABLE_%d>>", len(tableMap))
			tableMap[placeholder] = frag
			placeholders = append(placeholders, placeholder)
		}
		out := strings.Join(placeholders, "\n")
		if strings.HasSuffix(match, "\n") {
			out += "\n"
		}
		return out
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

// splitOversizedTable 将超过 tableSplitThreshold 的表格按数据行边界预切为多张完整子表。
//
// 规则：
//   - 表格 ≤ 阈值 → 返回 nil（不切，走整表保护）
//   - 行数 < 3（表头/分隔行不全的畸形表）→ 返回 nil（无法识别表结构，保持原样）
//   - 每张子表 = 表头行 + 分隔行 + 若干数据行，≤ 阈值（贪心装箱）
//   - 单个数据行自身超预算 → 整行独占一片，不切断行（仍带表头）
//   - 表头块自身超预算 → 每片仍拼表头（"尽量有表头"，片可能超阈值）
func (s *MarkdownPreservingSplitter) splitOversizedTable(table string) []string {
	if utf8.RuneCountInString(table) <= s.tableSplitThreshold {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(table, "\n"), "\n")
	if len(lines) < 3 {
		return nil
	}
	headerBlock := lines[0] + "\n" + lines[1] + "\n"
	budget := s.tableSplitThreshold - utf8.RuneCountInString(headerBlock)
	if budget <= 0 {
		// 表头块自身达到阈值：无法在阈值内装下任何数据行，按每片一行处理（仍尽量带表头）
		budget = 1
	}
	var fragments []string
	var cur []string // 当前片的缓冲（不含 headerBlock）
	curLen := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		fragments = append(fragments, strings.TrimSuffix(headerBlock+strings.Join(cur, "\n"), "\n"))
		cur = nil
		curLen = 0
	}
	for _, row := range lines[2:] {
		rowLen := utf8.RuneCountInString(row)
		if curLen > 0 && curLen+1+rowLen > budget {
			flush()
		}
		if len(cur) > 0 {
			curLen++ // 行间换行分隔符
		}
		cur = append(cur, row)
		curLen += rowLen
	}
	flush()
	return fragments
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
// 重叠机制（语义边界门槛）：
//
//	每块切出后，仅当块尾 chunkOverlap 窗口内存在语义边界（段落 > 换行 >
//	句末标点）时，下一块 = 边界之后的重叠尾巴 + 剩余文本；找不到语义
//	边界则下一块直接从剩余文本开始（零重叠）。
//	重叠起点因此必在句子/段落边界之后，不会落在句子中间。
//
// 切分策略：
//  1. 计算最少需要的块数（ceil(原始长度 / (chunkSize - chunkOverlap))）
//  2. 循环调用 findChunk 找到每个切分点
//  3. 应用重叠：仅当块尾存在语义边界时叠加重叠前缀
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

		// 应用重叠逻辑：仅当块尾窗口存在语义边界时，取边界之后的尾巴作为
		// 下一块的重叠前缀；找不到语义边界则零重叠（remaining = rest）。
		if s.chunkOverlap > 0 && rest != "" {
			if overlapText := s.semanticOverlap(chunk); overlapText != "" {
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

// semanticOverlapLookbehind 是重叠窗口额外回看的 rune 数，保证被窗口起点
// 截断的分隔符仍然可见（最长候选分隔符 "\n\n" 为 2 rune，取 4 留余量）。
const semanticOverlapLookbehind = 4

// semanticOverlap 在块的尾部窗口内寻找语义边界，返回边界分隔符之后的尾巴
// 作为下一块的重叠前缀；找不到语义边界时返回空串（本块与下一块之间零重叠）。
//
// 边界规则（移植自 WeKnora chunker computeOverlap 的语义门槛）：
//   - 优先级：段落分隔 "\n\n" > 换行 "\n" > 句末 "。！？" 或 ". " "! " "? "
//   - 优先级相同时取窗口内最早出现的边界，重叠尽量大
//   - 分隔符不得落在 <<TABLE_N>>/<<LINK_N>> 占位符内部
//   - 边界之后（窗口范围内）必须还有非空白内容，否则不产生重叠
//   - 重叠长度不超过 chunkOverlap（边界末尾必须落在最后 chunkOverlap 个 rune 内）
func (s *MarkdownPreservingSplitter) semanticOverlap(chunk string) string {
	chunkRunes := []rune(chunk)
	maxOverlap := s.chunkOverlap
	if maxOverlap > len(chunkRunes) {
		maxOverlap = len(chunkRunes)
	}
	if maxOverlap <= 0 {
		return ""
	}

	windowLen := maxOverlap + semanticOverlapLookbehind
	if windowLen > len(chunkRunes) {
		windowLen = len(chunkRunes)
	}
	runes := chunkRunes[len(chunkRunes)-windowLen:]
	minEnd := windowLen - maxOverlap // 边界分隔符的末尾必须落在最后 maxOverlap 个 rune 内
	placeholderRanges := s.findPlaceholderRanges(string(runes))

	type boundary struct {
		start, end, priority int
	}
	var best boundary
	found := false
	consider := func(start, end, priority int) {
		if start < 0 || end <= start || end < minEnd || end > len(runes) {
			return
		}
		// 分隔符不得切开占位符
		if isPositionInPlaceholder(start, placeholderRanges) ||
			isPositionInPlaceholder(end-1, placeholderRanges) {
			return
		}
		// 边界之后必须还有非空白内容，避免产生纯空白重叠
		if end >= len(runes) || strings.TrimSpace(string(runes[end:])) == "" {
			return
		}
		if !found || priority < best.priority ||
			(priority == best.priority && start < best.start) {
			best = boundary{start: start, end: end, priority: priority}
			found = true
		}
	}

	// 段落分隔 "\n\n"（优先级 1）；标记组成它的换行避免重复按换行（优先级 2）计
	paragraphRune := make([]bool, len(runes))
	for i := 0; i+1 < len(runes); i++ {
		if runes[i] == '\n' && runes[i+1] == '\n' {
			consider(i, i+2, 1)
			paragraphRune[i], paragraphRune[i+1] = true, true
			i++
		}
	}
	// 换行（优先级 2）
	for i := 0; i < len(runes); i++ {
		if paragraphRune[i] {
			continue
		}
		if runes[i] == '\n' {
			consider(i, i+1, 2)
		}
	}
	// 句末标点（优先级 3）：中文句号/感叹/问号单 rune 成界；英文标点需后随空格
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '。', '！', '？':
			consider(i, i+1, 3)
		case '.', '!', '?':
			if i+1 < len(runes) && runes[i+1] == ' ' {
				consider(i, i+2, 3)
			}
		}
	}

	if !found {
		return ""
	}
	overlap := string(runes[best.end:])
	if strings.TrimSpace(overlap) == "" {
		return ""
	}
	return overlap
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
// 子块重叠不取配置值，固定按 chunkSize/5 派生（移植自 WeKnora
// chunker.DeriveParentChildConfigs 的经验比例：约 20% 子块大小），
// 且带语义边界门槛——重叠起点必落在句子/段落边界之后。
//
// 参数：
//   - results: 一级分段结果列表
//   - chunkSize: 每个块的最大字符数（rune 计数）
//   - tableSplitThreshold: 表格原子保护单元的最大容忍字符数；超过该值的表格在保护阶段
//     预切为多张带表头子表。≤ 0 时取默认值 chunkSize
//   - isTocTitleResult: 是否为"有章节目录组织形式"的内容。
//     当为 true 时，会设置 ParentIndex 字段，用于标记子块与父块的关联关系
func SplitLargeBlocks(results []SplitResult, chunkSize, tableSplitThreshold int, isTocTitleResult bool) []SplitResult {
	if len(results) == 0 {
		return results
	}

	splitter := NewMarkdownPreservingSplitter(chunkSize, chunkSize/5)
	if tableSplitThreshold > 0 {
		splitter.SetTableSplitThreshold(tableSplitThreshold)
	}
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

		//var chunks []string
		//splitter := textsplitter.NewMarkdownTextSplitter(textsplitter.WithChunkSize(chunkSize), textsplitter.WithChunkOverlap(10),
		//	textsplitter.WithJoinTableRows(true),
		//	textsplitter.WithHeadingHierarchy(true),
		//	textsplitter.WithLenFunc(func(s string) int {
		//		return utf8.RuneCountInString(s)
		//	}),
		//)
		//docs, err := textsplitter.CreateDocuments(splitter, []string{string(result.Content)}, nil)
		//if err != nil {
		//	logx.Error("textsplitter.CreateDocuments error:%v", err)
		//	continue
		//}
		//
		//for _, item := range docs {
		//
		//	chunks = append(chunks, item.PageContent)
		//}

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
