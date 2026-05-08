package split

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// TitleSpecialChars defines special characters to remove from titles
var TitleSpecialChars = []string{"#", "\n", "\r", "\\s"}

// SplitResult represents a split paragraph
type SplitResult struct {
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	Keywords    []string `json:"keywords"`
	ParentChain []string `json:"parent_chain"`
	Level       int      `json:"level"`
	OriginLevel int      `json:"originLevel"` //原始level
	ParentIndex int      `json:"parentIndex"`
}

// TreeNode represents a node in the document tree
type TreeNode struct {
	Content  string      `json:"content"`
	State    string      `json:"state"` // "title" or "block"
	Level    int         `json:"level"`
	Children []*TreeNode `json:"children,omitempty"`
}

// SplitModel handles intelligent text splitting
type SplitModel struct {
	withFilter bool // 是否过滤特殊字符
	filterToc  bool // 是否过滤 PDF 目录条目
	limit      int  // 每个分段的最大字符数
}

// NewSplitModel creates a new SplitModel
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

// Parse parses the markdown text and returns split paragraphs
func (m *SplitModel) Parse(text string) []SplitResult {
	// Normalize line endings
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\x00", "")

	// Split into lines for better processing
	lines := strings.Split(text, "\n")

	// Build tree structure
	tree := m.parseLines(lines, 0)
	// Convert tree to flat paragraphs
	return m.treeToParagraphs(tree)
}

// parseLines parses lines into a tree structure
func (m *SplitModel) parseLines(lines []string, baseLevel int) []*TreeNode {
	var result []*TreeNode
	i := 0

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Skip empty lines
		if line == "" {
			i++
			continue
		}

		// Check if this line is a heading
		level := m.getHeadingLevel(line)

		if level > 0 {
			// It's a heading - extract title content
			title := m.extractTitleFromHeading(line, level)

			// Find content between this heading and the next
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
					break
				}
				contentLines = append(contentLines, lines[j])
				j++
			}

			// Create title node
			node := &TreeNode{
				Content: title,
				State:   "title",
				Level:   level - 1, // Convert 1-6 to 0-5 for consistency
			}

			// Recursively parse children if there's content
			if len(contentLines) > 0 {
				node.Children = m.parseLines(contentLines, level)
			}

			result = append(result, node)
			i = j
		} else {
			// It's content - collect until next heading or end
			var contentLines []string
			for i < len(lines) {
				trimmed := strings.TrimSpace(lines[i])
				// Stop if we hit a heading
				if m.getHeadingLevel(lines[i]) > 0 {
					break
				}
				// Skip TOC content but don't break
				if m.isTocContent(trimmed) {
					i++
					continue
				}
				// Add content (including empty lines which will be filtered later)
				contentLines = append(contentLines, lines[i])
				i++
				// Stop if we hit an empty line after collecting some content
				if trimmed == "" && len(contentLines) > 0 {
					break
				}
			}

			if len(contentLines) > 0 {
				content := strings.TrimSpace(strings.Join(contentLines, "\n"))
				if content != "" {
					// Split long content
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

// isTocContent checks if the line is TOC content
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

// isProbablyTocParagraph checks if a paragraph is likely part of TOC
// 检查策略：
//  1. 短文本（<100 字符）包含 TOC 标记
//  2. 文本中包含大量 TOC 标记（>2 个）
//  3. 如果启用了 filterToc，检查是否有多数行匹配 PDF 目录格式
func (m *SplitModel) isProbablyTocParagraph(content string) bool {
	if content == "" {
		return false
	}
	trimmed := strings.TrimSpace(content)
	// Short content that might be TOC entries
	if len(trimmed) < 100 && m.isTocContent(trimmed) {
		return true
	}
	// If most of the content is TOC markers
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

// getHeadingLevel returns the heading level (1-6) or 0 if not a heading
func (m *SplitModel) getHeadingLevel(line string) int {
	line = strings.TrimLeft(line, " \t")
	if len(line) < 2 || line[0] != '#' {
		return 0
	}

	// Count leading #
	count := 0
	for i := 0; i < len(line) && i < 6; i++ {
		if line[i] == '#' {
			count++
		} else if line[i] == ' ' || line[i] == '\t' {
			break
		} else {
			return 0 // Invalid heading (e.g., "##abc" without space)
		}
	}

	// Must be followed by space
	if count > 0 && len(line) > count && (line[count] == ' ' || line[count] == '\t') {
		// Additional check: ensure there's actual content after the heading markers
		titleContent := strings.TrimSpace(line[count:])
		if len(titleContent) == 0 {
			return 0 // Empty heading like "## " with no title
		}
		return count
	}

	return 0
}

// extractTitleFromHeading extracts the title text from a heading line
func (m *SplitModel) extractTitleFromHeading(line string, level int) string {
	// Remove leading # and space
	trimmed := strings.TrimLeft(line, " \t")
	prefix := strings.Repeat("#", level) + " "
	if strings.HasPrefix(trimmed, prefix) {
		title := trimmed[len(prefix):]
		title = strings.TrimRight(title, " \t")
		return m.filterTitleSpecialChars(title)
	}
	return strings.TrimSpace(trimmed[level:])
}

// smartSplitParagraph splits long content at sentence boundaries (rune-aware)
func (m *SplitModel) smartSplitParagraph(content string, limit int) []string {
	runes := []rune(content)
	if len(runes) <= limit {
		return []string{content}
	}

	var result []string
	start := 0

	for start < len(runes) {
		end := start + limit

		if end >= len(runes) {
			result = append(result, strings.TrimSpace(string(runes[start:])))
			break
		}

		// Find best split point within range (rune positions)
		bestSplit := m.findBestSplitPoint(runes, start, end)

		result = append(result, strings.TrimSpace(string(runes[start:bestSplit])))
		start = bestSplit
	}

	// Filter empty strings
	filtered := make([]string, 0, len(result))
	for _, s := range result {
		s = strings.TrimSpace(s)
		if s != "" {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// findBestSplitPoint finds the best position to split text (rune-based)
func (m *SplitModel) findBestSplitPoint(runes []rune, start, end int) int {
	// Split characters as runes (each is exactly one rune entry)
	splitRunes := []struct {
		char   rune
		offset int
	}{
		{'。', 1},  // Chinese period
		{'.', 1},  // English period
		{'！', 1},  // Chinese exclamation
		{'!', 1},  // English exclamation
		{'？', 1},  // Chinese question
		{'?', 1},  // English question
		{'；', 1},  // Chinese semicolon
		{';', 1},  // English semicolon
		{'\n', 1}, // Newline
	}

	// Search from end to start for best split point
	for i := end - 1; i >= start; i-- {
		for _, sc := range splitRunes {
			if runes[i] == sc.char {
				return i + sc.offset
			}
		}
	}

	// No good split point found, use default
	if end < len(runes) {
		return end
	}
	return len(runes)
}

// treeToParagraphs converts the tree to flat paragraphs
func (m *SplitModel) treeToParagraphs(nodes []*TreeNode) []SplitResult {
	var result []SplitResult
	var parentChain []string

	var walk func([]*TreeNode, []string)
	walk = func(currentNodes []*TreeNode, chain []string) {
		for _, node := range currentNodes {
			if node.State == "title" {
				titleContent := node.Content
				newChain := append(chain, titleContent)

				// If title has no children (empty content), output title as content so it's not lost
				if len(node.Children) == 0 {
					if titleContent != "" {
						titleStr := strings.Join(chain, " ")
						level := len(chain)
						result = append(result, SplitResult{
							Title:       titleStr,
							Content:     titleContent,
							ParentChain: chain,
							Level:       level,
							OriginLevel: node.Level,
						})
					}
					continue
				}

				// Process children with updated chain
				walk(node.Children, newChain)
			} else { // block
				content := node.Content
				if m.withFilter {
					content = m.filterSpecialChars(content)
				}

				content = strings.TrimSpace(content)
				if content == "" {
					continue
				}

				// title: all parent titles joined with space (consistent with Python)
				titleStr := strings.Join(chain, " ")
				// level: depth in parent chain (consistent with Python)
				level := len(chain)

				// Handle content longer than 4096 runes
				if utf8.RuneCountInString(content) > 4096 {
					blocks := m.smartSplitParagraph(content, 4096)
					for _, block := range blocks {
						block = strings.TrimSpace(block)
						if block != "" {
							// Skip TOC content
							if m.isProbablyTocParagraph(block) {
								continue
							}
							result = append(result, SplitResult{
								Title:       titleStr,
								Content:     block,
								ParentChain: chain,
								Level:       level,
								OriginLevel: node.Level,
							})
						}
					}
				} else {
					// Skip TOC content
					if m.isProbablyTocParagraph(content) {
						continue
					}
					result = append(result, SplitResult{
						Title:       titleStr,
						Content:     content,
						ParentChain: chain,
						Level:       level,
						OriginLevel: node.Level,
					})
				}
			}
		}
	}

	walk(nodes, parentChain)

	// Post-process: filter empty content
	filtered := make([]SplitResult, 0, len(result))
	for _, r := range result {
		// Skip entries with empty content and empty title
		if r.Content == "" && r.Title == "" {
			continue
		}
		filtered = append(filtered, r)
	}

	return filtered
}

// filterTitleSpecialChars removes special characters from title
func (m *SplitModel) filterTitleSpecialChars(title string) string {
	for _, char := range TitleSpecialChars {
		title = strings.ReplaceAll(title, char, "")
	}
	return strings.TrimSpace(title)
}

// filterSpecialChars removes special characters from content
func (m *SplitModel) filterSpecialChars(content string) string {
	// Don't filter newlines for table content (starts with |)
	isTable := strings.HasPrefix(content, "|")

	replacements := []*regexp.Regexp{
		regexp.MustCompile(`\n+`), // Replace multiple newlines with single newline (preserve line breaks)
		regexp.MustCompile(` +`),  // Replace multiple spaces with single space
		regexp.MustCompile(`#`),
		regexp.MustCompile(`\t+`),
	}

	for _, re := range replacements {
		// Skip newline replacement for tables to preserve row structure
		if isTable && re.String() == `\n+` {
			continue
		}
		if re.String() == `\n+` {
			content = re.ReplaceAllString(content, "\n") // Keep newlines, just collapse multiple
		} else if re.String() == ` +` {
			content = re.ReplaceAllString(content, " ") // Collapse multiple spaces
		} else {
			content = re.ReplaceAllString(content, "")
		}
	}

	return strings.TrimSpace(content)
}
