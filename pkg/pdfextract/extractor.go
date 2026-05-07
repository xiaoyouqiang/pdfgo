// Package pdfextract 提供纯 Go 实现的 PDF 内容提取功能。
//
// 功能概述：
//   - 从 PDF 文件中提取文本、表格和图片
//   - 使用 pdfcpu 库解析 PDF 结构
//   - 通过内容流解释器解码文本字符
//   - 通过布局分析器将字符组织为文本行和文本框
//   - 通过表格检测器识别表格结构
//
// 典型用法：
//
//	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true, ExtractTable: true})
//	result, _ := e.ExtractFile("input.pdf")
//	markdown := pdfextract.PagesToMarkdown(result.Pages)
package pdfextract

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpu "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	pdfcpuModel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/font"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/interpret"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/layout"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/table"
)

// ExtractionOptions 配置 PDF 提取行为，控制提取哪些类型的内容。
type ExtractionOptions struct {
	ExtractText  bool  // 是否提取文本内容
	ExtractTable bool  // 是否检测和提取表格
	ExtractImage bool  // 是否提取图片
	Pages        []int // 指定提取的页码（nil 表示所有页面）
}

// DefaultExtractionOptions 返回默认提取选项（仅提取文本）。
func DefaultExtractionOptions() ExtractionOptions {
	return ExtractionOptions{
		ExtractText:  true,
		ExtractTable: false,
		ExtractImage: false,
	}
}

// Extractor 是 PDF 提取器，负责协调各个子模块完成 PDF 内容提取。
// 它是 pdfextract 包的主要入口点。
type Extractor struct {
	opts ExtractionOptions
}

// NewExtractor 使用给定的选项创建一个新的 PDF 提取器。
func NewExtractor(opts ExtractionOptions) *Extractor {
	return &Extractor{opts: opts}
}

// ExtractFile 从 PDF 文件中提取内容，返回包含文档标题和每页结构化数据的结果。
func (e *Extractor) ExtractFile(path string) (*model.ExtractionResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	return e.ExtractReader(f)
}

// ExtractBytes 从 PDF 字节数据中提取内容。
func (e *Extractor) ExtractBytes(data []byte) (*model.ExtractionResult, error) {
	return e.ExtractReader(strings.NewReader(string(data)))
}

// ExtractReader 从 io.ReadSeeker 中提取 PDF 内容。
// 这是核心提取方法，处理流程如下：
//  1. 使用 pdfcpu 读取并验证 PDF 结构
//  2. 遍历指定页面（或所有页面），逐页提取内容
//  3. 返回每页的结构化数据
func (e *Extractor) ExtractReader(r io.ReadSeeker) (*model.ExtractionResult, error) {
	// 使用宽松验证模式，兼容不完全规范的 PDF 文件
	conf := pdfcpuModel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpuModel.ValidationRelaxed

	var ctx *pdfcpuModel.Context
	var err error
	// 如果需要提取图片，执行优化以确保 XObject 可用
	if e.opts.ExtractImage {
		conf.Optimize = true
		ctx, err = api.ReadValidateAndOptimize(r, conf)
	} else {
		ctx, err = api.ReadAndValidate(r, conf)
	}
	if err != nil {
		return nil, fmt.Errorf("read PDF: %w", err)
	}

	// 确定要提取的页码列表
	pageCount := ctx.PageCount
	var pageNums []int
	if e.opts.Pages != nil {
		pageNums = e.opts.Pages
	} else {
		pageNums = make([]int, pageCount)
		for i := 0; i < pageCount; i++ {
			pageNums[i] = i + 1 // PDF 页码从 1 开始
		}
	}

	// 逐页提取内容
	var pages []model.Page
	for _, pgNum := range pageNums {
		if pgNum < 1 || pgNum > pageCount {
			continue
		}
		page, err := e.extractPage(ctx, pgNum)
		if err != nil {
			fmt.Printf("warning: failed to extract page %d: %v\n", pgNum, err)
			continue
		}
		pages = append(pages, *page)
	}

	// 从第一页居中文本中识别文档标题
	title := detectTitle(pages)

	return &model.ExtractionResult{
		Title: title,
		Pages: pages,
	}, nil
}

// extractPage 提取单页 PDF 的内容。
//
// 处理流程：
//  1. 获取页面字典和属性（尺寸、资源等）
//  2. 读取页面内容流字节
//  3. 构建字体解析器，解析页面中使用的所有字体
//  4. 运行内容流解释器，提取字符、矩形、线段等原始数据
//  5. 运行表格检测器（如启用）
//  6. 构建文本框（排除表格区域的字符）
//  7. 提取图片（如启用）
func (e *Extractor) extractPage(ctx *pdfcpuModel.Context, pageNum int) (*model.Page, error) {
	// 获取页面字典和继承属性
	pageDict, _, inhPAttrs, err := ctx.PageDict(pageNum, false)
	if err != nil {
		return nil, fmt.Errorf("get page dict: %w", err)
	}

	// 获取页面尺寸（宽度和高度）
	var width, height float64
	if inhPAttrs.MediaBox != nil {
		width = inhPAttrs.MediaBox.Width()
		height = inhPAttrs.MediaBox.Height()
	}

	// 读取页面内容流的原始字节
	contentBytes, err := ctx.PageContent(pageDict, pageNum)
	if err != nil || len(contentBytes) == 0 {
		// 空白页面，直接返回基本信息
		return &model.Page{
			PageNum: pageNum,
			Width:   width,
			Height:  height,
		}, nil
	}

	// 构建字体解析器：遍历页面资源中的字体字典，
	// 为每种字体创建对应的解码器（简单字体或 CID 复合字体）
	fontResolver := font.NewFontResolver()
	e.buildFonts(ctx, inhPAttrs.Resources, fontResolver)

	// 创建内容流解释器并执行解释
	interpreter := interpret.NewInterpreter(fontResolver.AllFonts())
	// 如果需要提取图片，传入 XObject 映射表
	if e.opts.ExtractImage {
		interpreter.SetXObjects(resolveXObjects(ctx, inhPAttrs.Resources))
	}
	result := interpreter.Interpret(contentBytes)

	// 表格检测：基于解释器输出的矩形和线段，检测表格结构
	var tables []model.Table
	if e.opts.ExtractTable {
		settings := table.DefaultSettings()
		settings.PageWidth = width
		settings.PageHeight = height
		tables = table.Detect(result, settings)
	}

	// 文本框构建：将字符分组为文本行，再分组为文本框
	var textBoxes []model.TextBox
	if e.opts.ExtractText && len(result.Chars) > 0 {
		chars := result.Chars
		// 如果检测到表格，先排除表格区域内的字符
		if len(tables) > 0 {
			chars = excludeTableChars(chars, tables)
		}
		// 使用布局分析器将字符组织为文本框
		textBoxes = e.buildTextBoxes(chars)
		// 如果检测到表格，将跨越表格 Y 范围的文本框拆分
		if len(tables) > 0 {
			textBoxes = splitTextBoxesOnTables(textBoxes, tables)
		}
	}

	// 图片提取：从 PDF XObject 中提取图片数据
	var images []model.ImageInfo
	if e.opts.ExtractImage {
		images = e.extractImages(ctx, pageNum, result.ImagePlacements)
	}

	return &model.Page{
		PageNum:   pageNum,
		Width:     width,
		Height:    height,
		TextBoxes: textBoxes,
		Tables:    tables,
		Images:    images,
	}, nil
}

// buildFonts 遍历页面资源中的字体字典，为每种字体创建解码器并注册到解析器中。
//
// 处理流程：
//  1. 从页面资源中查找 Font 字典
//  2. 遍历每种字体，根据 Subtype 分类处理
//  3. Type0（CID 复合字体）：调用 buildCIDFont 处理
//  4. Type1/TrueType/Type3（简单字体）：解析 ToUnicode CMap 和宽度表
func (e *Extractor) buildFonts(ctx *pdfcpuModel.Context, resources types.Dict, resolver *font.FontResolver) {
	if resources == nil {
		return
	}
	fontObj, found := resources.Find("Font")
	if !found {
		return
	}
	fontDict, ok := fontObj.(types.Dict)
	if !ok {
		return
	}
	for name, entry := range fontDict {
		if entry == nil {
			continue
		}
		// 获取字体的间接引用
		indRef, ok := entry.(types.IndirectRef)
		if !ok {
			continue
		}
		// 解引用获取字体字典
		fd, err := ctx.DereferenceFontDict(indRef)
		if err != nil {
			continue
		}
		// 获取字体子类型
		subtype := ""
		if s := fd.NameEntry("Subtype"); s != nil {
			subtype = *s
		}

		// Type0 是复合字体（CID），需要特殊处理
		if subtype == "Type0" {
			e.buildCIDFont(ctx, fd, name, resolver)
			continue
		}
		// 只处理 Type1、TrueType 和 Type3 简单字体
		if subtype != "Type1" && subtype != "TrueType" && subtype != "Type3" {
			continue
		}
		baseFont := ""
		if s := fd.NameEntry("BaseFont"); s != nil {
			baseFont = *s
		}

		// 尝试解析 ToUnicode CMap（用于将字符编码映射为 Unicode）
		var cmap *font.CMap
		if toUniRef := fd.IndirectRefEntry("ToUnicode"); toUniRef != nil {
			sd, _, err := ctx.DereferenceStreamDict(*toUniRef)
			if err == nil && sd != nil {
				if err := sd.Decode(); err == nil {
					cmap, _ = font.ParseCMap(sd.Content)
				}
			}
		}

		// 构建字符宽度映射表（用于计算字符的前进宽度）
		widths := make(map[byte]float64)
		if wArr := fd.ArrayEntry("Widths"); len(wArr) > 0 {
			firstChar := 0
			if fi := fd.IntEntry("FirstChar"); fi != nil {
				firstChar = *fi
			}
			for i, w := range wArr {
				if num, ok := w.(types.Integer); ok && firstChar+i < 256 {
					widths[byte(firstChar+i)] = float64(num) / 1000.0 // 宽度单位为千分之一
				}
				if num, ok := w.(types.Float); ok && firstChar+i < 256 {
					widths[byte(firstChar+i)] = num.Value()
				}
			}
		}

		// 创建简单字体解码器并注册
		decoder := font.NewSimpleFontDecoder(baseFont, cmap, nil, widths, subtype)
		resolver.Register(name, decoder)
	}
}

// buildCIDFont 构建 Type0（CID）复合字体的解码器。
// CID 字体使用 ToUnicode CMap 将多字节字符编码映射为 Unicode。
func (e *Extractor) buildCIDFont(ctx *pdfcpuModel.Context, type0Dict types.Dict, resName string, resolver *font.FontResolver) {
	// 解析 ToUnicode CMap
	var cmap *font.CMap
	if toUniRef := type0Dict.IndirectRefEntry("ToUnicode"); toUniRef != nil {
		sd, _, err := ctx.DereferenceStreamDict(*toUniRef)
		if err == nil && sd != nil {
			if err := sd.Decode(); err == nil {
				cmap, _ = font.ParseCMap(sd.Content)
			}
		}
	}
	// 没有 CMap 则无法解码字符，跳过
	if cmap == nil {
		return
	}

	baseFont := ""
	if s := type0Dict.NameEntry("BaseFont"); s != nil {
		baseFont = *s
	}

	// 获取默认字符宽度（来自 DescendantFonts 中的 CIDFont 字典）
	dw := 1.0
	descArr := type0Dict.ArrayEntry("DescendantFonts")
	if len(descArr) > 0 {
		if cidRef, ok := descArr[0].(types.IndirectRef); ok {
			cidDict, err := ctx.DereferenceDict(cidRef)
			if err == nil {
				if dwVal := cidDict.IntEntry("DW"); dwVal != nil {
					dw = float64(*dwVal) / 1000.0
				}
			}
		}
	}

	// 创建 CID 字体解码器并注册
	decoder := font.NewCIDFontDecoder(baseFont, cmap, dw)
	resolver.Register(resName, decoder)
}

// buildTextBoxes 使用布局分析器将字符分组为文本框。
// 布局分析器会先将字符按 Y 坐标分组为文本行，再将相邻行分组为文本框。
func (e *Extractor) buildTextBoxes(chars []model.Char) []model.TextBox {
	return layout.Analyze(chars, layout.DefaultParams())
}

// excludeTableChars 从字符列表中排除落在表格单元格内的字符，
// 避免表格内容被重复包含在文本框中。
func excludeTableChars(chars []model.Char, tables []model.Table) []model.Char {
	var filtered []model.Char
	for _, ch := range chars {
		// 使用字符边界框的中心点判断是否在表格内
		mx := (ch.BBox.X0 + ch.BBox.X1) / 2
		my := (ch.BBox.Y0 + ch.BBox.Y1) / 2
		inTable := false
		for _, tbl := range tables {
			for r := 0; r < tbl.Rows; r++ {
				for c := 0; c < tbl.Cols; c++ {
					if tbl.Cells[r][c].BBox.Contains(model.Point{X: mx, Y: my}) {
						inTable = true
						break
					}
				}
				if inTable {
					break
				}
			}
			if inTable {
				break
			}
		}
		if !inTable {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}

// splitTextBoxesOnTables 拆分跨越表格 Y 范围的文本框。
// 当一个文本框的行分布在整个表格的上下方时，将其拆分为独立的文本框，
// 确保表格区域内的文本不会被混入普通文本框中。
func splitTextBoxesOnTables(boxes []model.TextBox, tables []model.Table) []model.TextBox {
	var result []model.TextBox
	for _, box := range boxes {
		split := splitBoxOnTables(box, tables)
		result = append(result, split...)
	}
	return result
}

// splitBoxOnTables 将单个文本框按表格边界拆分为多个文本框。
// 在表格 Y 范围内的行被丢弃，表格上方和下方的行各自形成新的文本框。
func splitBoxOnTables(box model.TextBox, tables []model.Table) []model.TextBox {
	if len(box.Lines) <= 1 || len(tables) == 0 {
		return []model.TextBox{box}
	}

	var groups [][]model.TextLine // 拆分后的行组
	var current []model.TextLine  // 当前正在积累的行组

	for i, line := range box.Lines {
		// 检查该行是否位于某个表格内部
		lineY0, lineY1 := lineYRange(line)
		insideTable := false
		for _, tbl := range tables {
			if lineY0 >= tbl.BBox.Y0 && lineY1 <= tbl.BBox.Y1 {
				insideTable = true
				break
			}
		}
		if insideTable {
			// 遇到表格内的行，结束当前行组
			if len(current) > 0 {
				groups = append(groups, current)
				current = nil
			}
			continue
		}

		// 检查当前行与上一行之间是否有表格
		if len(current) > 0 {
			prevY0, prevY1 := lineYRange(current[len(current)-1])
			prevBottom := math.Min(prevY0, prevY1)
			curTop := math.Max(lineY0, lineY1)
			for _, tbl := range tables {
				tblTop := tbl.BBox.Y1
				tblBottom := tbl.BBox.Y0
				// 表格位于上一行（上方）和当前行（下方）之间
				if tblTop <= prevBottom && tblBottom >= curTop {
					if len(current) > 0 {
						groups = append(groups, current)
						current = nil
					}
					break
				}
			}
		}

		current = append(current, line)
		_ = i
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	// 只有一组行，无需拆分
	if len(groups) <= 1 {
		return []model.TextBox{box}
	}

	// 为每组行创建独立的文本框
	var out []model.TextBox
	for _, lines := range groups {
		tb := model.TextBox{Lines: lines}
		if len(lines) > 0 {
			// 根据所有行的边界框计算整体边界框
			bbox := lines[0].BBox
			for _, l := range lines[1:] {
				bbox.X0 = math.Min(bbox.X0, l.BBox.X0)
				bbox.Y0 = math.Min(bbox.Y0, l.BBox.Y0)
				bbox.X1 = math.Max(bbox.X1, l.BBox.X1)
				bbox.Y1 = math.Max(bbox.Y1, l.BBox.Y1)
			}
			tb.BBox = bbox
		}
		out = append(out, tb)
	}
	return out
}

// lineYRange 计算一行文本的 Y 坐标范围（最小 Y0 到最大 Y1）。
func lineYRange(line model.TextLine) (y0, y1 float64) {
	if len(line.Chars) == 0 {
		return line.BBox.Y0, line.BBox.Y1
	}
	y0 = line.Chars[0].BBox.Y0
	y1 = line.Chars[0].BBox.Y1
	for _, c := range line.Chars[1:] {
		if c.BBox.Y0 < y0 {
			y0 = c.BBox.Y0
		}
		if c.BBox.Y1 > y1 {
			y1 = c.BBox.Y1
		}
	}
	return y0, y1
}

// resolveXObjects 从页面资源中解析 XObject 映射表。
// 仅保留 Image 类型的 XObject，用于后续图片提取。
func resolveXObjects(ctx *pdfcpuModel.Context, resources types.Dict) map[string]int {
	result := make(map[string]int)
	if resources == nil {
		return result
	}
	xobjEntry, found := resources.Find("XObject")
	if !found {
		return result
	}
	xobjDict, ok := xobjEntry.(types.Dict)
	if !ok {
		return result
	}
	for name, entry := range xobjDict {
		indRef, ok := entry.(types.IndirectRef)
		if !ok {
			continue
		}
		objNr := indRef.ObjectNumber.Value()
		// 检查是否为 Image 类型的 XObject
		sd, _, err := ctx.DereferenceStreamDict(indRef)
		if err != nil || sd == nil {
			continue
		}
		subtype := sd.Dict.NameEntry("Subtype")
		if subtype != nil && *subtype == "Image" {
			result[name] = objNr
		}
	}
	return result
}

// extractImages 提取指定页面中的所有图片，并与解释器记录的绘制位置匹配。
// 使用 pdfcpu 的 ExtractPageImages 获取图片元数据和实际数据。
func (e *Extractor) extractImages(ctx *pdfcpuModel.Context, pageNum int, placements []model.ImagePlacement) []model.ImageInfo {
	// 第一次提取：获取图片元数据（尺寸等）
	stubs, err := pdfcpu.ExtractPageImages(ctx, pageNum, true)
	if err != nil {
		return nil
	}
	// 第二次提取：获取实际图片数据
	pdfcpuImgs, err := pdfcpu.ExtractPageImages(ctx, pageNum, false)
	if err != nil || len(pdfcpuImgs) == 0 {
		return nil
	}

	// 构建图片位置映射表（对象编号 → 页面位置）
	placementMap := make(map[int]model.Rect)
	for _, p := range placements {
		placementMap[p.ObjNr] = p.BBox
	}

	var images []model.ImageInfo
	for objNr, img := range pdfcpuImgs {
		// 读取图片的二进制数据
		var data []byte
		if img.Reader != nil {
			data, _ = io.ReadAll(img.Reader)
		}
		if len(data) == 0 {
			continue
		}

		// 确定图片格式
		format := img.FileType
		if format == "" {
			format = "png"
		}

		info := model.ImageInfo{
			Format: format,
			Data:   data,
		}

		// 从 stub 元数据中获取图片尺寸
		if stub, ok := stubs[objNr]; ok {
			info.Width = stub.Width
			info.Height = stub.Height
		}

		// 匹配图片在页面上的位置
		if bbox, ok := placementMap[objNr]; ok {
			info.BBox = bbox
		}

		images = append(images, info)
	}
	return images
}

// SaveImages 将所有提取的图片保存到指定目录。
// 文件名格式为 prefix + UUID前8位 + 格式扩展名。
// 保存成功后，会更新 ImageInfo 的 SavedFilename 字段。
func SaveImages(pages []model.Page, outputDir string, prefix string) error {
	// 创建输出目录（如不存在）
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	for pi := range pages {
		for ii := range pages[pi].Images {
			img := &pages[pi].Images[ii]
			// 确定文件扩展名
			ext := "." + img.Format
			if ext == ".jpg" {
				ext = ".jpeg"
			}
			// 生成唯一文件名
			uid := uuid.New().String()[:8]
			filename := prefix + uid + ext
			path := outputDir + string(os.PathSeparator) + filename
			// 写入图片文件
			if err := os.WriteFile(path, img.Data, 0644); err != nil {
				return fmt.Errorf("write image: %w", err)
			}
			// 记录保存的文件名，供 Markdown 引用
			img.SavedFilename = filename
		}
	}
	return nil
}

// detectTitle 从第一页顶部文本中识别文档标题。
//
// 识别策略：
//  1. 统计跨页重复出现的文本行，识别为页眉/页脚
//  2. 取第一页按阅读顺序排列的第一个非页眉页脚的非空文本行
//  3. 判断该行是否居中（行中心 X 坐标接近页面中心，容差为页面宽度的 10%）
//  4. 如果居中，则认为该行是文档标题；否则认为文档没有标题
func detectTitle(pages []model.Page) string {
	if len(pages) == 0 {
		return ""
	}

	firstPage := pages[0]
	if len(firstPage.TextBoxes) == 0 {
		return ""
	}

	// 构建页眉页脚集合：统计每条规范化文本出现在多少个不同页面上
	headerFooterSet := buildHeaderFooterSet(pages)

	pageMidX := firstPage.Width / 2
	tolerance := firstPage.Width * 0.10 // 居中容差：页面宽度的 10%

	// 取第一个非页眉页脚的非空文本行
	for _, tb := range firstPage.TextBoxes {
		for _, line := range tb.Lines {
			text := strings.TrimSpace(line.Text())
			if text == "" {
				continue
			}
			// 跳过页眉页脚
			if headerFooterSet[normalizeTitleText(text)] {
				continue
			}
			// 找到第一个有效行，判断是否居中
			lineMidX := (line.BBox.X0 + line.BBox.X1) / 2
			if math.Abs(lineMidX-pageMidX) <= tolerance {
				return text
			}
			// 第一个有效行不居中，说明文档没有标题
			return ""
		}
	}
	return ""
}

// buildHeaderFooterSet 统计跨页重复出现的文本行，返回页眉页脚文本集合。
// 出现在 >= max(3, 页数/2) 个页面上的文本行被认为是页眉或页脚。
func buildHeaderFooterSet(pages []model.Page) map[string]bool {
	set := make(map[string]bool)
	if len(pages) < 3 {
		return set
	}

	threshold := len(pages) / 2
	if threshold < 3 {
		threshold = 3
	}

	linePageCount := make(map[string]int)
	for _, page := range pages {
		seen := make(map[string]bool)
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

	for norm, count := range linePageCount {
		if count >= threshold {
			set[norm] = true
		}
	}
	return set
}

// normalizeTitleText 移除所有空白字符，用于文本比较。
func normalizeTitleText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r != ' ' && r != '\n' && r != '\r' && r != '\t' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
