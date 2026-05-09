package pdfextract

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// PagesToMarkdown 将提取的 PDF 页面数据转换为 Markdown 格式字符串。
//
// 处理流程：
//  1. 调用 FilterHeadersFooters 移除重复的页眉页脚
//  2. 调用 findBodyFontSize 计算正文字体大小（众数）
//  3. 调用 buildHeadingTiers 自动发现标题字体大小层级
//  4. 按阅读顺序遍历每页的内容项（文本、表格、图片），生成 Markdown
//
// 标题检测策略：
//   - 优先使用 buildHeadingTiers 的自动分层结果
//   - 如果没有可用的层级信息，回退到固定阈值判断
//
// 表格渲染为 Markdown 管道表格格式，图片使用 Markdown 图片链接。
func PagesToMarkdown(pages []model.Page) string {
	// 第一步：过滤页眉页脚
	FilterHeadersFooters(pages)
	// 第二步：计算正文字体大小
	bodySize := findBodyFontSize(pages)
	// 第三步：自动发现标题层级
	tiers := buildHeadingTiers(pages, bodySize)

	var sb strings.Builder
	for pi, page := range pages {
		// 构建文本框查找映射表（BBox → TextBox）
		tbMap := buildTextBoxMap(page.TextBoxes)

		// 按视觉阅读顺序遍历内容项
		for _, item := range page.ReadingOrder() {
			switch item.Type {
			case "text":
				// 在映射表中查找对应的文本框
				tb := findTextBox(tbMap, item.BBox)
				if tb != nil {
					writeTextBoxMarkdown(&sb, *tb, bodySize, tiers)
				} else {
					// 未找到文本框，直接写入原始文本
					text := strings.TrimSpace(item.Text)
					if text != "" {
						sb.WriteString(text)
						sb.WriteString("\n")
					}
				}
			case "table":
				// 渲染表格为 Markdown 管道表格
				writeTableMarkdown(&sb, item.Table)
			case "image":
				// 渲染图片为 Markdown 图片链接
				writeImageMarkdown(&sb, item.Image, pi)
			}
		}
	}
	return sb.String()
}

// FilterHeadersFooters 过滤跨多页重复出现的文本行，用于移除页眉和页脚。
//
// 算法：
//  1. 统计每条规范化文本（去除空白后）出现在多少个不同的页面上
//  2. 如果一条文本出现在 >= max(3, 页数/2) 个页面上，认为是页眉/页脚
//  3. 从所有页面的文本框中移除这些行
//  4. 移除后变为空的文本框将被整体丢弃
func FilterHeadersFooters(pages []model.Page) {
	// 页面数不足 3 页，不需要过滤
	if len(pages) < 3 {
		return
	}

	// 计算出现阈值：至少出现在 max(3, 总页数/2) 个页面上才认为是页眉页脚
	threshold := len(pages) / 2
	if threshold < 3 {
		threshold = 3
	}

	// 第一遍：统计每条规范化文本出现在多少个页面上
	linePageCount := make(map[string]int)
	for _, page := range pages {
		seen := make(map[string]bool) // 每页内去重，避免同页多次出现被重复计数
		for _, tb := range page.TextBoxes {
			for _, line := range tb.Lines {
				norm := normalizeTitleText(line.Text())
				if norm != "" && !seen[norm] {
					linePageCount[norm]++
					seen[norm] = true
				}
			}
		}
	}

	// 第二遍：移除达到阈值的行，丢弃空文本框
	for pi := range pages {
		var filtered []model.TextBox
		for _, tb := range pages[pi].TextBoxes {
			var keep []model.TextLine
			for _, line := range tb.Lines {
				norm := normalizeTitleText(line.Text())
				if linePageCount[norm] < threshold {
					keep = append(keep, line)
				}
			}
			if len(keep) > 0 {
				tb.Lines = keep
				filtered = append(filtered, tb)
			}
		}
		pages[pi].TextBoxes = filtered
	}
}

// headingTier 表示一个字体大小对应的标题层级。
// 用于将 PDF 中不同大小的标题字体映射为 Markdown 标题级别。
type headingTier struct {
	fontSize float64 // 字体大小
	level    int     // Markdown 标题级别：2 = H2, 3 = H3, 4 = H4
}

// buildHeadingTiers 自动发现 PDF 中大于正文字体的不同字体大小，
// 并按从大到小的顺序分配 Markdown 标题级别。
//
// 算法：
//  1. 遍历所有页面，收集所有字体大小大于正文 (+0.5) 的文本行
//  2. 仅考虑短文本行（<= 15 个词/字符），排除可能是正文的行
//  3. 将字体大小四舍五入为整数，统计每种大小的出现次数
//  4. 按字体大小降序排列
//  5. 最大的映射为 H2，第二大的映射为 H3，依此类推直到 H6
//
// 例如：字体大小 439 → H2，字体大小 319 → H3，字体大小 209 → 正文
func buildHeadingTiers(pages []model.Page, bodySize float64) []headingTier {
	const maxWords = 15 // 标题候选行的最大词数/字符数

	type sizeStat struct {
		size  float64
		count int
	}
	stats := make(map[float64]*sizeStat)

	for _, page := range pages {
		for _, tb := range page.TextBoxes {
			for _, line := range tb.Lines {
				fs := lineFontSize(line)
				// 跳过正文及以下的字体大小
				if fs <= bodySize+0.5 {
					continue
				}
				text := strings.TrimSpace(line.Text())
				if text == "" {
					continue
				}
				// 只统计短文本行，长行通常是正文而非标题
				wordCount := len(strings.Fields(text))
				// 对中文文本，额外检查字符数
				cjkCount := 0
				for _, r := range text {
					if r >= 0x4E00 && r <= 0x9FFF {
						cjkCount++
					}
				}
				if wordCount > maxWords || cjkCount > maxWords {
					continue
				}

				// 字体大小四舍五入为整数，聚合相近的大小
				rounded := math.Round(fs)
				if stats[rounded] == nil {
					stats[rounded] = &sizeStat{size: rounded}
				}
				stats[rounded].count++
			}
		}
	}

	if len(stats) == 0 {
		return nil
	}

	// 按字体大小降序排列
	sorted := make([]headingTier, 0, len(stats))
	for _, s := range stats {
		sorted = append(sorted, headingTier{fontSize: s.size})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].fontSize > sorted[j].fontSize
	})

	// 分配标题级别：最大 → H2，第二 → H3，...，最小 → H6
	for i := range sorted {
		level := 2 + i
		if level > 6 {
			level = 6
		}
		sorted[i].level = level
	}
	return sorted
}

// tierForSize 根据字体大小查找对应的标题级别。
// 先尝试精确匹配（误差 < 1），找不到则使用最近邻匹配。
// 返回 0 表示该字体大小属于正文文本。
func tierForSize(tiers []headingTier, fontSize, bodySize float64) int {
	// 正文及以下的字体大小返回 0
	if fontSize <= bodySize+0.5 {
		return 0
	}
	rounded := math.Round(fontSize)
	// 精确匹配
	for _, t := range tiers {
		if math.Abs(t.fontSize-rounded) < 1 {
			return t.level
		}
	}
	// 回退：找最近的层级
	bestLevel := 0
	bestDist := math.MaxFloat64
	for _, t := range tiers {
		d := math.Abs(t.fontSize - rounded)
		if d < bestDist {
			bestDist = d
			bestLevel = t.level
		}
	}
	return bestLevel
}

// findBodyFontSize 计算所有页面中出现频率最高的字体大小（众数），
// 作为正文字体大小的估计值。如果没有任何字体信息，返回默认值 12.0。
func findBodyFontSize(pages []model.Page) float64 {
	// 统计每种字体大小的出现次数
	counts := make(map[float64]int)
	for _, page := range pages {
		for _, tb := range page.TextBoxes {
			for _, line := range tb.Lines {
				fs := lineFontSize(line)
				if fs > 0 {
					// 四舍五入到 0.5 的精度，聚合相近的字体大小
					rounded := math.Round(fs*2) / 2
					counts[rounded]++
				}
			}
		}
	}
	if len(counts) == 0 {
		return 12.0
	}
	// 找出现次数最多的字体大小（众数）；次数相同时选择较小的字号（更可能是正文）
	best := 0.0
	bestN := 0
	for sz, n := range counts {
		if n > bestN || (n == bestN && sz < best) {
			best = sz
			bestN = n
		}
	}
	return best
}

// lineFontSize 返回文本行第一个字符的字体大小。
// 假设同一行内的所有字符使用相同的字体大小。
func lineFontSize(line model.TextLine) float64 {
	if len(line.Chars) == 0 {
		return 0
	}
	return line.Chars[0].Font.Size
}

// writeTextBoxMarkdown 将一个文本框转换为 Markdown 格式。
// 如果存在标题层级信息（tiers），使用自动分层结果判断标题级别；
// 否则回退到固定阈值（diff > 2 → H2, diff > 0.5 → H3）。
func writeTextBoxMarkdown(sb *strings.Builder, tb model.TextBox, bodySize float64, tiers []headingTier) {
	for _, line := range tb.Lines {
		text := line.Text()
		if text == "" {
			continue
		}
		fs := lineFontSize(line)
		text = strings.TrimSpace(text)

		if len(tiers) > 0 {
			// 使用自动分层结果
			level := tierForSize(tiers, fs, bodySize)
			if level > 0 {
				// 标题行：写入对应级别的 Markdown 标题
				sb.WriteString(strings.Repeat("#", level))
				sb.WriteString(" ")
				sb.WriteString(text)
				sb.WriteString("\n\n")
			} else {
				// 正文行
				sb.WriteString(text)
				sb.WriteString("\n")
			}
		} else {
			// 回退到固定阈值判断
			diff := fs - bodySize
			if diff > 2 {
				sb.WriteString("## ")
				sb.WriteString(text)
				sb.WriteString("\n\n")
			} else if diff > 0.5 {
				sb.WriteString("### ")
				sb.WriteString(text)
				sb.WriteString("\n\n")
			} else {
				sb.WriteString(text)
				sb.WriteString("\n")
			}
		}
	}
}

// writeTableMarkdown 将表格渲染为 Markdown 管道表格格式。
// 第一行作为表头，后续行作为数据行。
func writeTableMarkdown(sb *strings.Builder, tbl *model.Table) {
	if tbl == nil || tbl.Rows == 0 || tbl.Cols == 0 {
		return
	}

	// 写入表头行
	sb.WriteString("|")
	for c := 0; c < tbl.Cols; c++ {
		sb.WriteString(" ")
		sb.WriteString(cellText(tbl.Cells[0][c]))
		sb.WriteString(" |")
	}
	sb.WriteString("\n")

	// 写入分隔行
	sb.WriteString("|")
	for c := 0; c < tbl.Cols; c++ {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")

	// 写入数据行（从第二行开始）
	for r := 1; r < tbl.Rows; r++ {
		sb.WriteString("|")
		for c := 0; c < tbl.Cols; c++ {
			sb.WriteString(" ")
			sb.WriteString(cellText(tbl.Cells[r][c]))
			sb.WriteString(" |")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}

// cellText 提取单元格文本并进行 Markdown 转义。
// 将换行替换为空格，管道符进行转义以避免破坏表格格式。
func cellText(cell model.Cell) string {
	t := strings.TrimSpace(cell.Text)
	t = strings.ReplaceAll(t, "\n", " ")  // 换行替换为空格
	t = strings.ReplaceAll(t, "|", "\\|") // 转义管道符
	return t
}

// writeImageMarkdown 将图片渲染为 Markdown 图片链接。
// 如果图片已保存到文件，使用文件名；否则使用页面索引作为占位符。
func writeImageMarkdown(sb *strings.Builder, img *model.ImageInfo, pageIdx int) {
	if img == nil {
		return
	}
	name := img.SavedFilename
	if name == "" {
		name = fmt.Sprintf("image_%d", pageIdx)
	}
	sb.WriteString("![image](")
	sb.WriteString(name)
	sb.WriteString(")\n\n")
}

// buildTextBoxMap 构建文本框的查找映射表。
// 使用边界框的量化值作为键，支持快速查找。
// 当多个文本框量化到同一键时，保留所有候选以供 findTextBox 选择最佳匹配。
func buildTextBoxMap(tbs []model.TextBox) map[[4]int][]*model.TextBox {
	m := make(map[[4]int][]*model.TextBox, len(tbs))
	for i := range tbs {
		key := bboxKey(tbs[i].BBox)
		m[key] = append(m[key], &tbs[i])
	}
	return m
}

// findTextBox 在映射表中查找与指定边界框对应的文本框。
// 先尝试精确匹配，找不到则使用模糊匹配（误差 < 2）。
// 当有多个候选时，选择面积最接近的。
func findTextBox(tbMap map[[4]int][]*model.TextBox, bbox model.Rect) *model.TextBox {
	key := bboxKey(bbox)
	targetArea := bbox.Area()

	// 辅助函数：从候选列表中选择面积最接近的文本框
	pickBest := func(candidates []*model.TextBox) *model.TextBox {
		if len(candidates) == 1 {
			return candidates[0]
		}
		best := candidates[0]
		bestDiff := math.Abs(best.BBox.Area() - targetArea)
		for _, c := range candidates[1:] {
			diff := math.Abs(c.BBox.Area() - targetArea)
			if diff < bestDiff {
				best = c
				bestDiff = diff
			}
		}
		return best
	}

	// 精确匹配
	if candidates, ok := tbMap[key]; ok {
		return pickBest(candidates)
	}
	// 模糊匹配：遍历所有键，找到最接近的
	for k, candidates := range tbMap {
		if math.Abs(float64(k[0]-key[0])) < 2 &&
			math.Abs(float64(k[1]-key[1])) < 2 &&
			math.Abs(float64(k[2]-key[2])) < 2 &&
			math.Abs(float64(k[3]-key[3])) < 2 {
			return pickBest(candidates)
		}
	}
	return nil
}

// bboxKey 将边界框坐标量化为整数键。
// 将坐标乘以 10 后四舍五入，减少浮点精度误差的影响。
func bboxKey(r model.Rect) [4]int {
	const s = 10.0
	return [4]int{
		int(math.Round(r.X0 * s)),
		int(math.Round(r.Y0 * s)),
		int(math.Round(r.X1 * s)),
		int(math.Round(r.Y1 * s)),
	}
}
