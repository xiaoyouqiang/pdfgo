// Package split 提供 Markdown 文本的智能分段功能。
//
// 核心功能：
//   - 将 Markdown 文本按标题层级结构解析为树结构
//   - 将树结构扁平化为分段结果（SplitResult）
//   - 支持长文本的智能切分（在句子边界处分割）
//   - 支持目录（TOC）内容的检测和过滤
//   - 支持特殊字符过滤
package split

import (
	"regexp"
	"strings"
)

// TitleSpecialChars 定义从标题中移除的特殊字符
var TitleSpecialChars = []string{"#", "\n", "\r", "\\s"}

// SplitResult 表示一个分段结果，是最终输出的核心数据结构。
type SplitResult struct {
	Title       string   `json:"title"`        // 段落的标题（取父链中最后一个标题）
	Content     string   `json:"content"`      // 段落的文本内容
	Keywords    []string `json:"keywords"`     // 关键词（暂未实现，始终为 nil）
	ParentChain []string `json:"parent_chain"` // 从顶级标题到当前标题的完整路径
	Level       int      `json:"level"`        // 标题深度（0-5）
}

// TreeNode 表示文档树中的一个节点，可以是标题或内容块。
type TreeNode struct {
	Content  string      `json:"content"`                  // 节点内容（标题文本或段落文本）
	State    string      `json:"state"`                    // 节点状态："title"（标题）或 "block"（内容块）
	Level    int         `json:"level"`                    // 标题深度（0-5）
	Children []*TreeNode `json:"children,omitempty"`       // 子节点列表
}

// SplitModel 是智能分段模型，管理分段的各种配置选项。
type SplitModel struct {
	withFilter bool // 是否过滤特殊字符
	filterToc  bool // 是否过滤 PDF 目录条目
	limit      int  // 每个分段的最大字符数
}

// NewSplitModel 创建一个新的分段模型。
//   - patterns: 正则表达式模式列表（当前未使用，保留扩展用）
//   - withFilter: 是否启用特殊字符过滤
//   - filterToc: 是否启用 PDF 目录过滤
//   - limit: 每个分段的最大字符数（50-100000）
func NewSplitModel(patterns []*regexp.Regexp, withFilter bool, filterToc bool, limit int) *SplitModel {
	if limit < 50 {
		limit = 50
	}
	if limit > 100000 {
		limit = 100000
	}
	return &SplitModel{
		withFilter: withFilter,
		filterToc:  filterToc,
		limit:      limit,
	}
}

// Parse 解析 Markdown 文本并返回分段结果。
//
// 处理流程：
//  1. 规范化换行符和移除空字符
//  2. 将文本按行切分
//  3. parseLines：将行解析为树结构（标题节点 + 内容节点）
//  4. treeToParagraphs：将树扁平化为分段结果数组
func (m *SplitModel) Parse(text string) []SplitResult {
	// 规范化换行符
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\x00", "")

	// 按行切分
	lines := strings.Split(text, "\n")

	// 构建树结构
	tree := m.parseLines(lines, 0)

	// 将树转换为扁平分段
	return m.treeToParagraphs(tree)
}

// parseLines 将 Markdown 行解析为树结构。
//
// 遍历每一行，根据内容分为两类：
//   - 标题行（以 # 开头）：创建标题节点，递归解析其下的内容
//   - 内容行：收集为内容块节点
//
// 标题层级通过 # 的数量判断（1-6），内容行中如果检测到 TOC 内容则跳过。
func (m *SplitModel) parseLines(lines []string, baseLevel int) []*TreeNode {
	var result []*TreeNode
	i := 0

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// 跳过空行
		if line == "" {
			i++
			continue
		}

		// 检查是否为标题行
		level := m.getHeadingLevel(line)

		if level > 0 {
			// 提取标题文本
			title := m.extractTitleFromHeading(line, level)

			// 收集该标题下的内容（直到遇到同级或更高级的标题）
			var contentLines []string
			j := i + 1
			for j < len(lines) {
				nextLine := strings.TrimSpace(lines[j])
				if nextLine == "" {
					j++
					continue
				}
				nextLevel := m.getHeadingLevel(lines[j])
				if nextLevel > 0 && nextLevel <= level {
					break // 遇到同级或更高级标题，停止收集
				}
				contentLines = append(contentLines, lines[j])
				j++
			}

			// 创建标题节点
			node := &TreeNode{
				Content: title,
				State:   "title",
				Level:   level - 1, // 将 1-6 转换为 0-5
			}

			// 递归解析子内容
			if len(contentLines) > 0 {
				node.Children = m.parseLines(contentLines, level)
			}

			result = append(result, node)
			i = j
		} else {
			// 内容行：收集直到遇到下一个标题
			var contentLines []string
			for i < len(lines) {
				trimmed := strings.TrimSpace(lines[i])
				// 遇到标题则停止
				if m.getHeadingLevel(lines[i]) > 0 {
					break
				}
				// 跳过 TOC 内容但不中断收集
				if m.isTocContent(trimmed) {
					i++
					continue
				}
				contentLines = append(contentLines, lines[i])
				i++
				// 收集到内容后遇到空行则停止
				if trimmed == "" && len(contentLines) > 0 {
					break
				}
			}

			if len(contentLines) > 0 {
				content := strings.TrimSpace(strings.Join(contentLines, "\n"))
				if content != "" {
					// 对超长内容进行智能切分
					blocks := m.smartSplitParagraph(content, m.limit)
					for _, block := range blocks {
						block = strings.TrimSpace(block)
						if block != "" {
							result = append(result, &TreeNode{
								Content: block,
								State:   "block",
								Level:   baseLevel,
							})
						}
					}
				}
			}
		}
	}

	return result
}

// isTocContent 检查一行文本是否为 Word 文档的 TOC（目录）标记。
// 这些标记通常是 Word 文档内部用于维护目录功能的特殊文本。
func (m *SplitModel) isTocContent(line string) bool {
	line = strings.ToLower(line)
	return strings.Contains(line, "toc") ||
		strings.Contains(line, "hyperlink") ||
		strings.Contains(line, "pager ef") ||
		strings.Contains(line, "\\o") ||
		strings.Contains(line, "\\h") ||
		strings.Contains(line, "\\u") ||
		strings.Contains(line, "_toc") ||
		strings.Contains(line, "_ref") ||
		strings.Contains(line, "_tab") ||
		line == "目录" ||
		line == "table of contents"
}

// pdfTocLineRe 匹配 PDF 风格的目录行，如 "背景...............1"
// 模式：1-80 个任意字符 + 3 个以上点号 + 可选空格 + 数字
var pdfTocLineRe = regexp.MustCompile(`^.{1,80}[\.。…]{3,} *\d+\s*$`)

// isPdfTocLine 检测 PDF 风格的目录条目行。
// 匹配格式："标题文本……页码" 或单独的 "目录"
func (m *SplitModel) isPdfTocLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "目录" || trimmed == "目 录" {
		return true
	}
	return pdfTocLineRe.MatchString(trimmed)
}

// isProbablyTocParagraph 判断一个段落是否可能是目录内容。
// 检查策略：
//  1. 短文本（<100 字符）包含 TOC 标记
//  2. 文本中包含大量 TOC 标记（>2 个）
//  3. 如果启用了 filterToc，检查是否有多数行匹配 PDF 目录格式
func (m *SplitModel) isProbablyTocParagraph(content string) bool {
	if content == "" {
		return false
	}
	trimmed := strings.TrimSpace(content)
	// 短文本包含 TOC 标记
	if len(trimmed) < 100 && m.isTocContent(trimmed) {
		return true
	}
	// 包含大量 Word TOC 标记
	tocMarkers := 0
	for _, marker := range []string{"\\o", "\\h", "\\u", "_Toc", "_Ref", "HYPERLINK", "PAGEREF"} {
		tocMarkers += strings.Count(content, marker)
	}
	if tocMarkers > 2 {
		return true
	}
	// PDF 风格目录：检查是否多数行匹配点号引导模式
	if m.filterToc {
		lines := strings.Split(content, "\n")
		tocLines := 0
		for _, line := range lines {
			if m.isPdfTocLine(line) {
				tocLines++
			}
		}
		if len(lines) > 0 && tocLines >= len(lines)/2 && tocLines >= 2 {
			return true
		}
	}
	return false
}

// getHeadingLevel 判断一行文本是否为 Markdown 标题，返回标题级别（1-6）。
// 非标题返回 0。有效标题格式：1-6 个 # 后跟空格和内容。
func (m *SplitModel) getHeadingLevel(line string) int {
	line = strings.TrimLeft(line, " \t")
	if len(line) < 2 || line[0] != '#' {
		return 0
	}

	// 计算前导 # 的数量
	count := 0
	for i := 0; i < len(line) && i < 6; i++ {
		if line[i] == '#' {
			count++
		} else if line[i] == ' ' || line[i] == '\t' {
			break
		} else {
			return 0 // 无效标题（如 "##abc" 没有空格分隔）
		}
	}

	// # 后必须跟空格，且空格后必须有内容
	if count > 0 && len(line) > count && (line[count] == ' ' || line[count] == '\t') {
		titleContent := strings.TrimSpace(line[count:])
		if len(titleContent) == 0 {
			return 0 // 空标题如 "## "
		}
		return count
	}

	return 0
}

// extractTitleFromHeading 从标题行中提取标题文本（移除 # 前缀和特殊字符）
func (m *SplitModel) extractTitleFromHeading(line string, level int) string {
	trimmed := strings.TrimLeft(line, " \t")
	prefix := strings.Repeat("#", level) + " "
	if strings.HasPrefix(trimmed, prefix) {
		title := trimmed[len(prefix):]
		title = strings.TrimRight(title, " \t")
		return m.filterTitleSpecialChars(title)
	}
	return strings.TrimSpace(trimmed[level:])
}

// smartSplitParagraph 在句子边界处智能切分长文本。
// 如果文本不超过 limit，直接返回；否则从后向前查找最近的句子结束符进行切分。
func (m *SplitModel) smartSplitParagraph(content string, limit int) []string {
	if len(content) <= limit {
		return []string{content}
	}

	var result []string
	start := 0

	for start < len(content) {
		end := start + limit

		if end >= len(content) {
			result = append(result, strings.TrimSpace(content[start:]))
			break
		}

		// 在范围内查找最佳切分点
		bestSplit := m.findBestSplitPoint(content, start, end)

		result = append(result, strings.TrimSpace(content[start:bestSplit]))
		start = bestSplit
	}

	// 过滤空字符串
	filtered := make([]string, 0, len(result))
	for _, s := range result {
		s = strings.TrimSpace(s)
		if s != "" {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// findBestSplitPoint 在指定范围内从后向前查找最佳的文本切分点。
// 优先在中英文句号、感叹号、问号、分号等句子结束符处切分。
func (m *SplitModel) findBestSplitPoint(content string, start, end int) int {
	// 按优先级排列的切分字符
	splitChars := []struct {
		char   string
		offset int
	}{
		{"。", 1}, // 中文句号
		{".", 1}, // 英文句号
		{"！", 1}, // 中文感叹号
		{"!", 1}, // 英文感叹号
		{"？", 1}, // 中文问号
		{"?", 1}, // 英文问号
		{"；", 1}, // 中文分号
		{";", 1}, // 英文分号
	}

	// 从后向前搜索切分点
	for i := end - 1; i >= start; i-- {
		for _, sc := range splitChars {
			if i+sc.offset <= len(content) && content[i:i+sc.offset] == sc.char {
				return i + sc.offset
			}
		}
	}

	// 未找到合适的切分点，使用默认位置
	if end < len(content) {
		return end
	}
	return len(content)
}

// treeToParagraphs 将文档树扁平化为分段结果数组。
// 递归遍历树结构，为每个内容块节点生成一个 SplitResult。
// 标题节点会更新父链（parentChain），内容块节点使用父链生成最终的分段。
func (m *SplitModel) treeToParagraphs(nodes []*TreeNode) []SplitResult {
	var result []SplitResult
	var parentChain []string

	var walk func([]*TreeNode, []string)
	walk = func(currentNodes []*TreeNode, chain []string) {
		for _, node := range currentNodes {
			if node.State == "title" {
				titleContent := node.Content
				newChain := append(chain, titleContent)

				// 标题没有子内容时，用标题文本作为 content 保留
				if len(node.Children) == 0 {
					result = append(result, SplitResult{
						Title:       titleContent,
						Content:     titleContent,
						ParentChain: newChain,
						Level:       node.Level,
					})
					continue
				}

				// 递归处理子节点
				walk(node.Children, newChain)
			} else { // 内容块
				content := node.Content
				if m.withFilter {
					content = m.filterSpecialChars(content)
				}

				content = strings.TrimSpace(content)
				if content == "" {
					continue
				}

				// 超长内容再次切分
				if len(content) > 4096 {
					blocks := m.smartSplitParagraph(content, 4096)
					for _, block := range blocks {
						block = strings.TrimSpace(block)
						if block != "" {
							titleStr := ""
							if len(chain) > 0 {
								titleStr = chain[len(chain)-1]
							}
							// 跳过目录内容
							if m.isProbablyTocParagraph(block) {
								continue
							}
							result = append(result, SplitResult{
								Title:       titleStr,
								Content:     block,
								ParentChain: chain,
								Level:       node.Level,
							})
						}
					}
				} else {
					titleStr := ""
					if len(chain) > 0 {
						titleStr = chain[len(chain)-1]
					}
					// 跳过目录内容
					if m.isProbablyTocParagraph(content) {
						continue
					}
					result = append(result, SplitResult{
						Title:       titleStr,
						Content:     content,
						ParentChain: chain,
						Level:       node.Level,
					})
				}
			}
		}
	}

	walk(nodes, parentChain)

	// 后处理：过滤空内容
	filtered := make([]SplitResult, 0, len(result))
	for _, r := range result {
		if r.Content == "" && r.Title == "" {
			continue
		}
		filtered = append(filtered, r)
	}

	return filtered
}

// filterTitleSpecialChars 移除标题中的特殊字符
func (m *SplitModel) filterTitleSpecialChars(title string) string {
	for _, char := range TitleSpecialChars {
		title = strings.ReplaceAll(title, char, "")
	}
	return strings.TrimSpace(title)
}

// filterSpecialChars 过滤内容中的特殊字符。
// 包括：折叠多个连续换行、折叠多个连续空格、移除 # 号和制表符。
// 对于 Markdown 表格内容（以 | 开头），保留换行符以维护表格行结构。
func (m *SplitModel) filterSpecialChars(content string) string {
	// 表格内容不折叠换行
	isTable := strings.HasPrefix(content, "|")

	replacements := []*regexp.Regexp{
		regexp.MustCompile(`\n+`), // 折叠多个连续换行
		regexp.MustCompile(` +`),  // 折叠多个连续空格
		regexp.MustCompile(`#`),   // 移除 # 号
		regexp.MustCompile(`\t+`), // 移除制表符
	}

	for _, re := range replacements {
		// 表格内容跳过换行折叠
		if isTable && re.String() == `\n+` {
			continue
		}
		if re.String() == `\n+` {
			content = re.ReplaceAllString(content, "\n")
		} else if re.String() == ` +` {
			content = re.ReplaceAllString(content, " ")
		} else {
			content = re.ReplaceAllString(content, "")
		}
	}

	return strings.TrimSpace(content)
}
