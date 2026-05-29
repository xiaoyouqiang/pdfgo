package model

import (
	"sort"
)

// Char 是 PDF 内容流解释器输出的最小文本单元，对应 PDF 中的一个字符。
// 由文本显示操作符（Tj、TJ 等）产生，包含字符文本、位置和字体信息。
type Char struct {
	Text      string   // 字符的 Unicode 文本内容
	Origin    Point    // 字符基线起点在页面坐标系中的位置
	BBox      Rect     // 字符的边界框（在页面坐标系中）
	Font      FontInfo // 字符的字体信息
	Advance   float64  // 水平前进宽度（渲染后光标移动的距离）
	FormObjNr int      // 产生此字符的 Form XObject 对象编号（0=页面直接内容）
	SeqNo     int      // 字符在内容流中的绘制序号（用于双层渲染去重：后绘制的覆盖先绘制的）
	Clipped   bool     // 字符是否在裁剪路径内产生（W/W* 操作符后的隐藏层文本）
}

// TextLine 表示一行文本，由多个连续的 Char 组成。
// 布局分析器将 Y 坐标相近的字符归为同一行。
type TextLine struct {
	Chars []Char // 该行包含的所有字符
	BBox  Rect   // 整行的边界框
}

// Text 将该行所有字符拼接为一个字符串
func (l *TextLine) Text() string {
	if len(l.Chars) == 0 {
		return ""
	}
	sz := 0
	for _, c := range l.Chars {
		sz += len(c.Text)
	}
	b := make([]byte, 0, sz)
	for _, c := range l.Chars {
		b = append(b, c.Text...)
	}
	return string(b)
}

// FontInfo 返回该行第一个字符的字体信息，用于标题级别判断
func (l *TextLine) FontInfo() FontInfo {
	if len(l.Chars) == 0 {
		return FontInfo{}
	}
	return l.Chars[0].Font
}

// TextBox 表示一个文本块（段落级别），由多行文本组成。
// 布局分析器将垂直距离较近且有水平重叠的文本行归为同一个文本框。
type TextBox struct {
	Lines []TextLine // 文本框包含的所有文本行
	BBox  Rect       // 整个文本框的边界框
}

// Text 将文本框中所有行的文本拼接，行与行之间用换行符分隔。
// 只有当上一行以连字符结尾时才用空格连接（表示单词被断开跨行），
// 其他情况都用换行符连接，保持原始换行结构。
func (b *TextBox) Text() string {
	if len(b.Lines) == 0 {
		return ""
	}
	sz := 0
	for _, l := range b.Lines {
		sz += len(l.Text())
	}
	bld := make([]byte, 0, sz)
	for i, l := range b.Lines {
		if i > 0 {
			// 上一行末尾是连字符，单词跨行，用空格连接
			// 否则用换行符连接，保持原始换行结构
			if len(bld) > 0 && bld[len(bld)-1] == '-' {
				bld = append(bld, ' ')
			} else {
				bld = append(bld, '\n')
			}
		}
		bld = append(bld, l.Text()...)
	}
	return string(bld)
}

// Cell 表示表格中的一个单元格。
type Cell struct {
	BBox Rect   // 单元格的边界框
	Text string // 单元格内的文本内容
	Row  int    // 所在行索引（从 0 开始）
	Col  int    // 所在列索引（从 0 开始）
}

// Table 表示一个检测到的表格。
type Table struct {
	BBox  Rect     // 表格的整体边界框
	Cells [][]Cell // 二维数组存储单元格，Cells[row][col]
	Rows  int      // 总行数
	Cols  int      // 总列数
}

// ImageInfo 表示从 PDF 中提取的图片信息。
type ImageInfo struct {
	BBox          Rect   // 图片在页面上的位置
	Width         int    // 图片宽度（像素）
	Height        int    // 图片高度（像素）
	Format        string // 图片格式："jpg"、"png"、"tif"
	Data          []byte // 图片的二进制数据
	SavedFilename string // 调用 SaveImages 后设置的保存文件名
}

// ImagePlacement 记录 XObject 图片在页面上的绘制位置。
// 当内容流解释器遇到 Do 操作符绘制图片时产生。
type ImagePlacement struct {
	Name  string // 资源名称（如 "Im0"）
	ObjNr int    // PDF 对象编号
	BBox  Rect   // 根据 CTM 计算出的页面位置
}

// ContentItem 表示阅读顺序中的一个内容项，可以是文本块、表格或图片。
// 用于将页面上的不同类型内容按视觉顺序交错排列。
type ContentItem struct {
	Type  string      // 内容类型："text"（文本）、"table"（表格）或 "image"（图片）
	BBox  Rect        // 内容项的边界框
	Text  string      // 当 Type == "text" 时的文本内容
	Table *Table      // 当 Type == "table" 时的表格指针
	Image *ImageInfo  // 当 Type == "image" 时的图片指针
}

// ExtractionResult 表示整个 PDF 文档的提取结果。
type ExtractionResult struct {
	Title string // 文档标题（从第一页居中文本中识别）
	Pages []Page // 所有页面的提取结果
}

// Page 表示一页 PDF 的提取结果。
type Page struct {
	PageNum   int          // 页码（从 1 开始）
	Width     float64      // 页面宽度
	Height    float64      // 页面高度
	TextBoxes []TextBox    // 该页上的所有文本框
	Tables    []Table      // 该页上检测到的所有表格
	Images    []ImageInfo  // 该页上的所有图片
}

// ReadingOrder 将页面上的所有文本框、表格和图片按视觉阅读顺序排列。
//
// TextBoxes 已由布局分析器按内容流顺序排好，
// 笔位跟踪算法确保多栏 PDF 的左右栏被正确分离为独立的 TextBox。
// Table/Image 按视觉 Y 坐标插入到 TextBox 序列中的正确位置，
// 保持 TextBox 的内容流顺序不变。
func (p *Page) ReadingOrder() []ContentItem {
	// 1. 收集 TextBoxes（保持内容流顺序）
	var items []ContentItem
	for i := range p.TextBoxes {
		tb := &p.TextBoxes[i]
		items = append(items, ContentItem{
			Type: "text",
			BBox: tb.BBox,
			Text: tb.Text(),
		})
	}

	// 2. 收集非文本项（Tables + Images），按 Y0 排序
	type yItem struct {
		item ContentItem
		y0   float64
	}
	var nonText []yItem
	for i := range p.Tables {
		nonText = append(nonText, yItem{
			item: ContentItem{Type: "table", BBox: p.Tables[i].BBox, Table: &p.Tables[i]},
			y0:   p.Tables[i].BBox.Y0,
		})
	}
	for i := range p.Images {
		nonText = append(nonText, yItem{
			item: ContentItem{Type: "image", BBox: p.Images[i].BBox, Image: &p.Images[i]},
			y0:   p.Images[i].BBox.Y0,
		})
	}
	sort.Slice(nonText, func(i, j int) bool {
		return nonText[i].y0 < nonText[j].y0
	})

	// 3. 逐个插入到 TextBox 序列中的正确位置
	// 插入点：第一个已有项的 Y0 > 非文本项的 Y0（即第一个视觉上更靠下的项之前）
	for _, ni := range nonText {
		insertIdx := len(items)
		for k, it := range items {
			if it.BBox.Y0 > ni.item.BBox.Y0+1 {
				insertIdx = k
				break
			}
		}
		items = append(items, ContentItem{})
		copy(items[insertIdx+1:], items[insertIdx:])
		items[insertIdx] = ni.item
	}

	if len(items) == 0 {
		return nil
	}
	return items
}

// LineSegment 表示一条图形线段，用于表格检测。
// 从 PDF 内容流的描边路径操作（m、l、S 等）中提取。
type LineSegment struct {
	X0, Y0, X1, Y1 float64 // 线段的起点和终点坐标
}

// InterpretResult 是 PDF 内容流解释器的输出结果。
// 包含从一页 PDF 的内容流中提取的所有原始数据。
type InterpretResult struct {
	Chars           []Char           // 提取的所有字符
	Rects           []Rect           // 提取的所有矩形路径（用于表格检测）
	Lines           []LineSegment    // 提取的所有线段（用于表格检测）
	ImagePlacements []ImagePlacement // 图片的绘制位置信息
}
