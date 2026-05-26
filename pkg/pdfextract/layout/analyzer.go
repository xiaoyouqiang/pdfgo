// Package layout 实现字符到文本框的布局分析。
// 将内容流解释器输出的原始字符按空间位置关系组织为文本行和文本框，
// 为后续的 Markdown 转换和标题检测提供结构化数据。
package layout

import (
	"math"
	"sort"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// Params 控制布局分析的行为参数。
type Params struct {
	CharMargin float64 // 字符间距容差（已弃用，当前未使用）
	LineMargin float64 // 行间距容差倍数，控制相邻行是否归入同一文本框
	WordMargin float64 // 词间距容差倍数，控制字符之间何时插入空格
}

// DefaultParams 返回默认的布局分析参数
func DefaultParams() Params {
	return Params{
		CharMargin: 2.0,
		LineMargin: 0.5,
		WordMargin: 0.1,
	}
}

// Analyze 将字符分组为按阅读顺序排列的文本框。
//
// 处理流程：
//  1. groupCharsToLines：将字符按 Y 坐标分组为文本行
//  2. groupLinesToTextBoxes：将相邻文本行分组为文本框（段落）
//  3. sortTextBoxes：按视觉位置排序文本框
func Analyze(chars []model.Char, params Params) []model.TextBox {
	if len(chars) == 0 {
		return nil
	}

	lines := groupCharsToLines(chars, params)
	boxes := groupLinesToTextBoxes(lines, params)
	sortTextBoxes(boxes)

	return boxes
}

// estimateFontSize 从字符位置估算有效渲染字体大小。
// 收集所有字符的 Y 坐标，计算相邻 Y 坐标之间的间隔，
// 取中位数作为行高估计（行高约等于字体大小）。
func estimateFontSize(chars []model.Char) float64 {
	if len(chars) < 2 {
		return 12
	}

	// 收集所有 Y 坐标并排序
	ys := make([]float64, len(chars))
	for i, c := range chars {
		ys[i] = c.Origin.Y
	}
	sort.Float64s(ys)

	// 计算 Y 坐标间隔
	var gaps []float64
	for i := 1; i < len(ys); i++ {
		gap := ys[i] - ys[i-1]
		if gap > 0.5 { // 忽略亚像素抖动
			gaps = append(gaps, gap)
		}
	}

	if len(gaps) == 0 {
		// 所有字符在同一行，使用字符前进宽度作为估计
		for _, c := range chars {
			if c.Advance > 0 {
				return c.Advance
			}
		}
		return 12
	}

	// 中位数间隔是行高的良好估计
	sort.Float64s(gaps)
	return gaps[len(gaps)/2]
}

// groupCharsToLines 将字符按 Y 坐标分组为文本行。
//
// 算法：
//  1. 按 Y 降序（页面顶部优先）、X 升序排序字符
//  2. 使用紧 Y 阈值（3 单位）判断字符是否属于同一行
//  3. 使用 X 间隔阈值检测分栏（多列布局）
//  4. 检测上标行：如果某行字号明显更小、Y 位置明显更高、且 X 与前一行重叠，则合并到前一行
func groupCharsToLines(chars []model.Char, params Params) []model.TextLine {
	if len(chars) == 0 {
		return nil
	}

	// 按 Y 降序（顶部优先）、X 升序排序
	sorted := make([]model.Char, len(chars))
	copy(sorted, chars)
	sort.Slice(sorted, func(i, j int) bool {
		yi, yj := sorted[i].Origin.Y, sorted[j].Origin.Y
		if math.Abs(yi-yj) > 1.0 {
			return yi > yj // PDF 坐标系中 Y 值大的在上方
		}
		return sorted[i].Origin.X < sorted[j].Origin.X
	})

	// 估算有效字号（用于 X 间隔阈值）
	estSize := estimateFontSize(sorted)

	// Y 阈值：同一视觉行的字符 Y 差异不超过 3 个单位
	lineHeightThreshold := 3.0

	// 估算平均字符前进宽度，用于检测分栏间隔
	avgAdv := 0.0
	advCount := 0
	for _, c := range sorted {
		if c.Advance > 0 {
			avgAdv += c.Advance
			advCount++
		}
	}
	if advCount > 0 {
		avgAdv /= float64(advCount)
	}
	// 分栏间隔阈值：至少 8 个字符宽度或 2 倍字号
	colGapThreshold := avgAdv * 8
	if colGapThreshold < estSize*2 {
		colGapThreshold = estSize * 2
	}

	var lines []model.TextLine
	var currentLine []model.Char

	for _, ch := range sorted {
		if len(currentLine) == 0 {
			currentLine = append(currentLine, ch)
			continue
		}

		// 用当前行所有字符的 Y 中位数作为基准，避免首个字符异常导致误判
		ySum := 0.0
		for _, c := range currentLine {
			ySum += c.Origin.Y
		}
		refY := ySum / float64(len(currentLine))
		yGap := math.Abs(ch.Origin.Y - refY)

		// Y 差异超过阈值，开始新行
		if yGap > lineHeightThreshold {
			lines = append(lines, finalizeLine(currentLine, params))
			currentLine = []model.Char{ch}
			continue
		}

		// 同一 Y 区间：检查 X 间隔是否表示分栏
		last := currentLine[len(currentLine)-1]
		xGap := ch.Origin.X - last.Origin.X
		if xGap < 0 {
			xGap = -xGap
		}

		if xGap > colGapThreshold {
			// X 间隔过大，视为不同列
			lines = append(lines, finalizeLine(currentLine, params))
			currentLine = []model.Char{ch}
		} else {
			currentLine = append(currentLine, ch)
		}
	}
	if len(currentLine) > 0 {
		lines = append(lines, finalizeLine(currentLine, params))
	}

	// 后处理：合并上标行到主文本行
	lines = mergeSuperscriptLines(lines)

	return lines
}

// mergeSuperscriptLines 基于 SeqNo（内容流绘制顺序）逐字符合并上标行。
//
// 采用两遍处理：
//
//	第一遍：收集所有非上标行到 result（确保所有主文本行就绪）
//	第二遍：对每个上标字符按 SeqNo 找到前驱字符，插入到其之后
//
// 必须两遍处理的原因：PDF 内容流中上标与作者名是交错绘制的：
//
//	[(Zhiyong Wu )]TJ    → SeqNo 124-133（第二行作者）
//	4.338 Td              → 移到上标位置
//	[(1,2,†)]TJ           → SeqNo 134-138（上标，属于第二行作者）
//	-4.338 Td             → 返回
//	[(, Yiwen Guo )]TJ    → SeqNo 139-149
//	...
//
// 上标行（Y=692.35）在 Y 降序中排在第一作者行之后、第二作者行之前。
// 若单遍处理，第二作者行尚未进入 result，上标会错误地归入第一作者行。
// 两遍处理确保所有主文本行就绪后再做 SeqNo 匹配。
func mergeSuperscriptLines(lines []model.TextLine) []model.TextLine {
	if len(lines) < 2 {
		return lines
	}

	// 第一遍：分离上标行和主文本行
	var result []model.TextLine
	var superscriptLines []model.TextLine
	for _, line := range lines {
		if isSuperscriptBySize(line) {
			superscriptLines = append(superscriptLines, line)
		} else {
			result = append(result, line)
		}
	}

	if len(superscriptLines) == 0 {
		return result
	}

	// 第二遍：按 SeqNo 逐字符合并上标到主文本行
	for _, supLine := range superscriptLines {
		supChars := make([]model.Char, len(supLine.Chars))
		copy(supChars, supLine.Chars)
		sort.Slice(supChars, func(a, b int) bool {
			return supChars[a].SeqNo < supChars[b].SeqNo
		})

		for _, sc := range supChars {
			lineIdx, charIdx := findInsertPosition(result, sc.SeqNo)
			if lineIdx >= 0 && charIdx >= 0 {
				target := &result[lineIdx]
				insertAt := charIdx + 1
				target.Chars = append(
					target.Chars[:insertAt],
					append([]model.Char{sc}, target.Chars[insertAt:]...)...,
				)
				target.BBox = lineBBox(target.Chars)
			}
		}
	}

	return result
}

// isSuperscriptBySize 仅通过字号判断是否为上标行。
// 上标行的字号必须明显小于页面主流字号。
func isSuperscriptBySize(line model.TextLine) bool {
	avgSize := avgFontSizeInLine(line.Chars)
	if avgSize <= 0 {
		return false
	}
	// 页面主流字号通常 >= 8，上标通常 <= 8
	// 使用固定阈值区分：上标字号 < 8 且行内字符数较少
	if avgSize >= 8 {
		return false
	}
	// 排除过长的行（不太可能是纯上标）
	if len(line.Chars) > 30 {
		return false
	}
	return true
}

// findInsertPosition 在所有已有行中找到 SeqNo 最近的前驱字符位置。
// 返回 (行索引, 字符索引)，用于在 charIdx+1 处插入上标字符。
func findInsertPosition(result []model.TextLine, targetSeqNo int) (int, int) {
	bestLineIdx := -1
	bestCharIdx := -1
	bestSeq := -1

	for li, line := range result {
		for ci, c := range line.Chars {
			if c.SeqNo < targetSeqNo && c.SeqNo > bestSeq {
				bestLineIdx = li
				bestCharIdx = ci
				bestSeq = c.SeqNo
			}
		}
	}

	return bestLineIdx, bestCharIdx
}

// mergeBySeqNo 已弃用，由 mergeSuperscriptLines 中的逐字符插入逻辑替代。
// 保留此函数签名以避免外部引用编译错误。
func mergeBySeqNo(line *model.TextLine, superscriptChars []model.Char) {
	if line == nil || len(line.Chars) == 0 || len(superscriptChars) == 0 {
		return
	}
	line.Chars = append(line.Chars, superscriptChars...)
}

// avgFontSizeInLine 计算一行中所有字符的平均字号。
func avgFontSizeInLine(chars []model.Char) float64 {
	if len(chars) == 0 {
		return 0
	}
	var sum, count float64
	for _, c := range chars {
		if c.Font.Size > 0 {
			sum += c.Font.Size
			count++
		}
	}
	if count > 0 {
		return sum / count
	}
	return 0
}

// hasSameFontOverlap 检测同一字体中是否有字符在 X 方向上重叠。
// 普通PDF中同一字体的字符依次排列不会重叠；只有双层渲染PDF才会
// 在同一X位置绘制多个同字体字符（用于覆盖校正）。
func hasSameFontOverlap(chars []model.Char) bool {
	type fontSpan struct{ x0, x1 float64 }
	spans := make(map[string][]fontSpan)
	for _, c := range chars {
		x0 := c.Origin.X
		x1 := c.Origin.X + c.Advance
		s := spans[c.Font.Name]
		for _, existing := range s {
			overlap := math.Min(x1, existing.x1) - math.Max(x0, existing.x0)
			if overlap > 0 {
				return true
			}
		}
		spans[c.Font.Name] = append(s, fontSpan{x0, x1})
	}
	return false
}

// finalizeLine 完成一行文本的构建：排序字符，插入词间空格，计算边界框。
// 当检测到同字体X重叠时（双层渲染PDF），按绘制序号排序以保持内容流顺序；
// 否则按X坐标排序（普通PDF的正常布局顺序）。
//
// 特殊处理：检测上标字符（字号明显小于行内平均字号且位置明显偏高），
// 如果其水平范围与主文本行重叠，则将其归入主文本行，不单独成行。
// 这解决了 LaTeX PDF 中作者上标 affiliation 数字与姓名断开的问题。
func finalizeLine(chars []model.Char, params Params) model.TextLine {
	if hasSameFontOverlap(chars) {
		sort.Slice(chars, func(i, j int) bool {
			return chars[i].SeqNo < chars[j].SeqNo
		})
	} else {
		sort.Slice(chars, func(i, j int) bool {
			return chars[i].Origin.X < chars[j].Origin.X
		})
	}

	// 估算平均字符前进宽度
	avgAdvance := 0.0
	count := 0
	for _, c := range chars {
		if c.Advance > 0 {
			avgAdvance += c.Advance
			count++
		}
	}
	if count > 0 {
		avgAdvance /= float64(count)
	}

	// 估算行内平均字号（用于检测上标）
	avgFontSize := 0.0
	fontCount := 0
	for _, c := range chars {
		if c.Font.Size > 0 {
			avgFontSize += c.Font.Size
			fontCount++
		}
	}
	if fontCount > 0 {
		avgFontSize /= float64(fontCount)
	}

	// 在字符间隙过大处插入空格
	var result []model.Char
	for i, ch := range chars {
		if i > 0 && len(result) > 0 && avgAdvance > 0 {
			prev := result[len(result)-1]
			gap := ch.Origin.X - (prev.Origin.X + prev.Advance)
			// 间隙超过平均宽度的一定倍数时插入空格
			if gap > avgAdvance*params.WordMargin*10 {
				result = append(result, model.Char{
					Text:    " ",
					Origin:  model.Point{X: prev.Origin.X + prev.Advance, Y: prev.Origin.Y},
					Advance: gap,
					Font:    prev.Font,
				})
			}
		}
		result = append(result, ch)
	}

	var bbox model.Rect
	if len(result) > 0 {
		bbox = lineBBox(result)
	}

	return model.TextLine{Chars: result, BBox: bbox}
}

// mergeSuperscriptCharsIntoLine 将上标行的所有字符合并到主文本行末尾，
// 在主文本最后一个非空格字符和上标之间插入一个空格。
func mergeSuperscriptCharsIntoLine(line *model.TextLine, superscriptChars []model.Char) {
	if line == nil || len(line.Chars) == 0 || len(superscriptChars) == 0 {
		return
	}
	// 取主文本最后一个字符作为空格的参考位置
	lastChar := line.Chars[len(line.Chars)-1]
	// 在上标前插入空格
	line.Chars = append(line.Chars, model.Char{
		Text:    " ",
		Origin:  model.Point{X: lastChar.Origin.X + lastChar.Advance, Y: lastChar.Origin.Y},
		Advance: superscriptChars[0].Origin.X - (lastChar.Origin.X + lastChar.Advance),
		Font:    lastChar.Font,
	})
	line.Chars = append(line.Chars, superscriptChars...)
}

// lineBBox 计算一行字符的整体边界框。
// 综合考虑字符的 BBox 和 Origin 位置。
func lineBBox(chars []model.Char) model.Rect {
	if len(chars) == 0 {
		return model.Rect{}
	}
	x0, y0 := chars[0].BBox.X0, chars[0].BBox.Y0
	x1, y1 := chars[0].BBox.X1, chars[0].BBox.Y1
	for _, c := range chars[1:] {
		x0 = math.Min(x0, c.BBox.X0)
		y0 = math.Min(y0, c.BBox.Y0)
		x1 = math.Max(x1, c.BBox.X1)
		y1 = math.Max(y1, c.BBox.Y1)
	}
	// 同时考虑 Origin 点和字体大小，获得更准确的边界
	for _, c := range chars {
		x0 = math.Min(x0, c.Origin.X)
		y0 = math.Min(y0, c.Origin.Y-math.Abs(c.Font.Size)*0.2)
		x1 = math.Max(x1, c.Origin.X+c.Advance)
		y1 = math.Max(y1, c.Origin.Y+math.Abs(c.Font.Size)*0.8)
	}
	return model.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}
}

// groupLinesToTextBoxes 将相邻的文本行分组为文本框（段落）。
// 判断条件：垂直间距小于行高的 (1 + LineMargin) 倍，且水平方向有重叠。
func groupLinesToTextBoxes(lines []model.TextLine, params Params) []model.TextBox {
	if len(lines) == 0 {
		return nil
	}

	var boxes []model.TextBox
	var currentBox *model.TextBox

	for _, line := range lines {
		if currentBox == nil {
			currentBox = &model.TextBox{Lines: []model.TextLine{line}}
			continue
		}

		lastLine := currentBox.Lines[len(currentBox.Lines)-1]
		lastHeight := lastLine.BBox.Height()
		if lastHeight == 0 {
			lastHeight = 12
		}


			// 计算与上一行的垂直间距（上一行底部到当前行顶部的距离）
			vGap := lastLine.BBox.Y0 - line.BBox.Y1
		// 检查水平方向是否有重叠
		overlapStart := math.Max(lastLine.BBox.X0, line.BBox.X0)
		overlapEnd := math.Min(lastLine.BBox.X1, line.BBox.X1)
		hasHorzOverlap := overlapEnd > overlapStart

		// 垂直间距较小且有水平重叠，归入同一文本框
		if vGap < lastHeight*(1+params.LineMargin) && hasHorzOverlap {
			currentBox.Lines = append(currentBox.Lines, line)
		} else {
			// 间距过大或无水平重叠，开始新文本框
			currentBox.BBox = computeBoxBBox(currentBox.Lines)
			boxes = append(boxes, *currentBox)
			currentBox = &model.TextBox{Lines: []model.TextLine{line}}
		}
	}

	// 处理最后一个文本框
	if currentBox != nil {
		currentBox.BBox = computeBoxBBox(currentBox.Lines)
		boxes = append(boxes, *currentBox)
	}

	return boxes
}

// computeBoxBBox 计算文本框中所有行的整体边界框
func computeBoxBBox(lines []model.TextLine) model.Rect {
	if len(lines) == 0 {
		return model.Rect{}
	}
	bbox := lines[0].BBox
	for _, l := range lines[1:] {
		bbox.X0 = math.Min(bbox.X0, l.BBox.X0)
		bbox.Y0 = math.Min(bbox.Y0, l.BBox.Y0)
		bbox.X1 = math.Max(bbox.X1, l.BBox.X1)
		bbox.Y1 = math.Max(bbox.Y1, l.BBox.Y1)
	}
	return bbox
}

// sortTextBoxes 按视觉位置排序文本框：Y 值大的优先（页面上方），同高度时 X 小的优先（左侧）
func sortTextBoxes(boxes []model.TextBox) {
	sort.Slice(boxes, func(i, j int) bool {
		bi, bj := boxes[i].BBox, boxes[j].BBox
		if math.Abs(bi.Y0-bj.Y0) > 5 {
			return bi.Y0 > bj.Y0
		}
		return bi.X0 < bj.X0
	})
}
