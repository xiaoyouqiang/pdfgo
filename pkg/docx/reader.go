// Package docxextractor 提供了 Word (.docx) 文档的解析功能。
//
// 主要功能：
//   - 从文件路径、io.Reader 或字节流解析 Word 文档
//   - 提取段落（含标题层级识别）、表格、图片
//   - 自动检测文档标题（居中且具有标题特征的段落）
//   - 将文档内容转换为 Markdown 格式
//   - 提取并保存文档中嵌入的图片资源
//
// 依赖：
//   - github.com/ZeroHawkeye/wordZero：底层 docx 文档解析库
//
// 实现原理：
//
//	docx 文件本质上是一个 ZIP 压缩包，内部结构包含：
//	- word/document.xml     ：文档正文内容
//	- word/_rels/document.xml.rels：文档关系文件（图片 rId 与文件的映射）
//	- word/media/           ：嵌入的图片资源目录
//	本包先从 ZIP 包中提取图片和关系映射，再通过 wordZero 库解析文档结构。
package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ZeroHawkeye/wordZero/pkg/document"
)

// TitleLevel 表示标题的层级（1-6），对应 Markdown 中的 # ~ ######
// 0 表示该段落不是标题（NotATitle）
type TitleLevel int

const (
	NotATitle TitleLevel = 0 // 非标题段落
	Level1    TitleLevel = 1 // 一级标题，对应 Markdown: #
	Level2    TitleLevel = 2 // 二级标题，对应 Markdown: ##
	Level3    TitleLevel = 3 // 三级标题，对应 Markdown: ###
	Level4    TitleLevel = 4 // 四级标题，对应 Markdown: ####
	Level5    TitleLevel = 5 // 五级标题，对应 Markdown: #####
	Level6    TitleLevel = 6 // 六级标题，对应 Markdown: ######
)

// Paragraph 表示文档中的一个段落
type Paragraph struct {
	Level   TitleLevel // 标题层级，如果为 NotATitle 则表示普通段落
	Content string     // 段落的文本内容（图片位置用 {{IMAGE:filename}} 占位符表示）
	IsTable bool       // 是否属于表格（当前始终为 false，保留字段）
}

// Table 表示文档中的一个表格
type Table struct {
	Rows [][]string // 二维字符串数组，Rows[行索引][列索引] = 单元格文本
}

// Document 表示一个完整的 Word 文档解析结果
type Document struct {
	Title      string            // 文档标题（从前 10 个段落中查找居中且具有标题特征的段落）
	Paragraphs []Paragraph       // 所有段落的列表（按文档顺序，不含表格）
	Tables     []Table           // 所有表格的列表（按文档顺序）
	Elements   []DocumentElement // 有序的段落/表格混合列表，保持文档中的原始顺序
	Images     []Image           // 从文档中提取的所有图片
	ImagePath  string            // 图片输出目录（由调用方设置）
}

// Image 表示从文档中提取的一张图片
type Image struct {
	ID            string // 图片标识（使用 ZIP 中的文件名作为 ID，如 "image1.jpeg"）
	Filename      string // 原始文件名（如 "image1.jpeg"）
	Data          []byte // 图片的二进制数据
	SavedFilename string // 实际保存到磁盘时的文件名（可能包含 UUID 或前缀）
}

// isImagePlaceholder 检查文本是否为图片占位符
// 占位符格式为 {{IMAGE:filename}}，由 getParagraphText 函数在解析段落时生成
// 参数：
//   - text: 待检查的文本
//
// 返回值：
//   - string: 如果是占位符，返回占位符中的文件名部分
//   - bool: 是否为图片占位符
func isImagePlaceholder(text string) (string, bool) {
	if strings.HasPrefix(text, "{{IMAGE:") && strings.HasSuffix(text, "}}") {
		// 去掉 "{{IMAGE:" 前缀（9个字符）和 "}}" 后缀（2个字符）
		return text[9 : len(text)-2], true
	}
	return "", false
}

// maxImageSize 单个图片文件的最大允许大小（200MB）
const maxImageSize = 200 * 1024 * 1024

// maxDocumentSize 从 io.Reader 读取文档时的最大允许大小（500MB）
const maxDocumentSize = 500 * 1024 * 1024

// 预编译正则表达式，避免在循环中重复编译（问题5修复）
var (
	// reHeadingStyle 匹配 "heading" 或 "标题" 关键词后跟可选数字
	reHeadingStyle = regexp.MustCompile(`(?i)(?:heading|标题)\s*(\d*)`)
	// rePureDigit 匹配纯数字字符串
	rePureDigit = regexp.MustCompile(`^\d+$`)
	// reNumberedParagraph 匹配数字编号段落（如 "1. 审核目标"）
	reNumberedParagraph = regexp.MustCompile(`^\d+\.\s`)
	// reImagePlaceholder 匹配图片占位符 {{IMAGE:xxx}}
	reImagePlaceholder = regexp.MustCompile(`\{\{IMAGE:([^}]+)\}\}`)
)

// titleFontList 标题字体大小阈值表（单位：磅/pt）
// 用于在段落没有显式标题样式时，通过字号大小来推断标题层级
// 每个元素为 [最小值, 最大值) 的左闭右开区间
// 注意：Word 文档中的字号单位是半磅（half-points），需要除以 2 转换为磅
var titleFontList = [][]int{
	{36, 100}, // Level 1: 36pt 及以上（如初号、小初号字体）
	{26, 36},  // Level 2: 26pt ~ 36pt（如一号字体）
	{24, 26},  // Level 3: 24pt ~ 26pt（如小一号字体）
	{22, 24},  // Level 4: 22pt ~ 24pt（如二号字体）
	{18, 22},  // Level 5: 18pt ~ 22pt（如小二号字体）
	{16, 18},  // Level 6: 16pt ~ 18pt（如三号字体）
}

// getTitleLevelFromStyle 根据段落的样式名称（style name）判断标题层级
// Word 文档中标题段落通常带有 "Heading 1" ~ "Heading 6" 的样式名
// 中文版 Word 使用 "标题 1" ~ "标题 6"
// 部分版本还会使用纯数字 1~9 来表示标题级别（纯数字 10~19 为 TOC 样式，不在此处理）
// 参数：
//   - styleName: 段落的样式名称（如 "Heading1"、"标题 2"、"2" 等）
//
// 返回值：
//   - TitleLevel: 识别到的标题层级，如果无法识别则返回 NotATitle
func getTitleLevelFromStyle(styleName string) TitleLevel {
	if styleName == "" {
		return NotATitle
	}

	lowerStyle := strings.ToLower(styleName)

	// 匹配 "heading" 或 "标题" 关键词后跟可选数字（如 "Heading1"、"Heading 2"、"标题 3"）
	matches := reHeadingStyle.FindStringSubmatch(styleName)
	if len(matches) >= 1 {
		// 严格匹配：必须以 heading 或 "标题" 开头，排除含 "toc" 的样式误判
		if strings.HasPrefix(lowerStyle, "heading") || strings.HasPrefix(lowerStyle, "标题") {
			// 提取样式名中的数字作为层级
			if len(matches) >= 2 && matches[1] != "" {
				var level int
				fmt.Sscanf(matches[1], "%d", &level)
				if level >= 1 && level <= 6 {
					return TitleLevel(level)
				}
			}
			// 样式匹配但没有数字，默认为一级标题
			return Level1
		}
	}

	// 处理纯数字样式值的情况（1~9 为标题，10~19 已由 isTocParagraph 处理为 TOC）
	if rePureDigit.MatchString(styleName) {
		var level int
		fmt.Sscanf(styleName, "%d", &level)
		if level >= 1 && level <= 6 {
			return TitleLevel(level)
		}
	}

	return NotATitle
}

// getTitleLevelFromFontSize 根据字号和是否加粗来判断标题层级
// 只有同时满足加粗且字号在 titleFontList 定义的范围内才认为是标题
// 参数：
//   - fontSizeStr: 字号值字符串（Word 使用半磅单位，如 "28" 表示 14pt）
//   - isBold: 是否加粗
//
// 返回值：
//   - TitleLevel: 识别到的标题层级，如果无法识别则返回 NotATitle
func getTitleLevelFromFontSize(fontSizeStr string, isBold bool) TitleLevel {
	// 必须同时有字号且加粗才可能是标题
	if fontSizeStr == "" || !isBold {
		return NotATitle
	}

	// 解析字号值（Word 中的字号单位是半磅）
	fontSize, err := strconv.Atoi(fontSizeStr)
	if err != nil || fontSize <= 0 {
		return NotATitle
	}

	// 将半磅转换为磅，然后与阈值表比较
	pt := fontSize / 2

	// 遍历阈值表，找到匹配的区间
	// 索引 i 对应标题层级 i+1
	for i, threshold := range titleFontList {
		if pt >= threshold[0] && pt < threshold[1] {
			return TitleLevel(i + 1)
		}
	}
	return NotATitle
}

// getParagraphText 从 wordZero 的 Paragraph 对象中提取文本内容
// 如果段落中包含图片（Drawing 元素），会用 {{IMAGE:filename}} 占位符替换图片位置
// 参数：
//   - para: wordZero 库的段落对象
//   - ridToFilename: rId 到文件名的映射表（从文档关系文件中解析得到）
//
// 返回值：
//   - string: 段落的完整文本内容
func getParagraphText(para *document.Paragraph, ridToFilename map[string]string) string {
	var result string
	// 遍历段落中的每个 Run（Word 中的一段连续格式文本）
	for _, run := range para.Runs {
		// 如果 Run 包含绘图元素（即图片）
		if run.Drawing != nil {
			// 从绘图元素中提取图片的关系 ID（rId）
			if rid := extractImageRId(run.Drawing); rid != "" {
				// 通过 rId 查找对应的文件名
				if filename, ok := ridToFilename[rid]; ok {
					// 用占位符替代图片位置
					result += fmt.Sprintf("{{IMAGE:%s}}", filename)
				}
			}
			continue // 跳过当前 Run，不处理其文本内容
		}
		// 普通 Run，直接拼接文本
		result += run.Text.Content
	}
	return result
}

// extractImageRId 从绘图元素（DrawingElement）中提取图片的关系 ID（rId）
// Word 文档中的图片可能以两种方式嵌入：
//  1. Inline（行内图片）：图片作为行内元素出现在文本流中
//  2. Anchor（锚定图片）：图片浮动在页面的指定位置
//
// 两种方式都需要深入多层嵌套结构才能获取到 Embed 字段（即 rId）
// XML 路径：Drawing -> Inline/Anchor -> Graphic -> GraphicData -> Pic -> BlipFill -> Blip -> Embed
// 参数：
//   - drawing: wordZero 的绘图元素对象
//
// 返回值：
//   - string: 图片的关系 ID，如 "rId4"；如果无法提取则返回空字符串
func extractImageRId(drawing *document.DrawingElement) string {
	if drawing == nil {
		return ""
	}
	// 优先尝试行内图片（Inline）
	if drawing.Inline != nil && drawing.Inline.Graphic != nil &&
		drawing.Inline.Graphic.GraphicData != nil &&
		drawing.Inline.Graphic.GraphicData.Pic != nil &&
		drawing.Inline.Graphic.GraphicData.Pic.BlipFill != nil &&
		drawing.Inline.Graphic.GraphicData.Pic.BlipFill.Blip != nil {
		return drawing.Inline.Graphic.GraphicData.Pic.BlipFill.Blip.Embed
	}
	// 尝试锚定图片（Anchor）
	if drawing.Anchor != nil && drawing.Anchor.Graphic != nil &&
		drawing.Anchor.Graphic.GraphicData != nil &&
		drawing.Anchor.Graphic.GraphicData.Pic != nil &&
		drawing.Anchor.Graphic.GraphicData.Pic.BlipFill != nil &&
		drawing.Anchor.Graphic.GraphicData.Pic.BlipFill.Blip != nil {
		return drawing.Anchor.Graphic.GraphicData.Pic.BlipFill.Blip.Embed
	}
	return ""
}

// getCellText 从表格单元格中提取文本内容
// 单元格中可能包含多个段落，各段落之间用 </br> 连接
// 注意：当前未使用此函数，表格文本提取改用 wordZero 库的 Table.GetCellText 方法
// 参数：
//   - cell: wordZero 的表格单元格对象
//   - ridToFilename: rId 到文件名的映射表
//
// 返回值：
//   - string: 单元格的完整文本内容
func getCellText(cell *document.TableCell, ridToFilename map[string]string) string {
	if len(cell.Paragraphs) == 0 {
		return ""
	}

	var parts []string
	for _, para := range cell.Paragraphs {
		parts = append(parts, getParagraphText(&para, ridToFilename))
	}
	// 多个段落之间用 HTML 换行标签连接
	return strings.Join(parts, "</br>")
}

// ParseWordFile 从文件路径解析 Word 文档
// 解析流程：
//  1. 从 ZIP 包中提取所有图片（word/media/ 目录下的文件）
//  2. 从 ZIP 包中解析关系文件（word/_rels/document.xml.rels），建立 rId -> 文件名的映射
//  3. 通过 wordZero 库解析文档结构（段落、表格等）
//  4. 遍历文档元素，识别标题层级、提取文本和表格内容
//
// 参数：
//   - filePath: Word 文件的绝对路径
//
// 返回值：
//   - *Document: 解析后的文档对象
//   - error: 解析过程中的错误
func ParseWordFile(filePath string) (*Document, error) {
	// 第一步：从 ZIP 包中提取所有嵌入的图片
	images := extractImagesFromZip(filePath)

	// 第二步：构建 rId 到文件名的映射表
	// 这个映射表用于将段落中的图片引用（rId）替换为实际的文件名
	ridToFilename := buildRidToFilenameMap(filePath)

	// 第三步：通过 wordZero 库打开并解析文档结构
	doc, err := document.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	// 第四步：将 wordZero 的文档结构转换为我们自己的 Document 格式
	result, err := parseWordZeroDocumentWithRidMap(doc, ridToFilename)
	if err != nil {
		return nil, err
	}

	// 将提取的图片附加到结果中
	result.Images = images
	return result, nil
}

// ParseWordDocument 从 io.Reader 解析 Word 文档
// 内部会先读取全部数据到内存，再调用 ParseWordBytes 进行解析
// 参数：
//   - r: 实现 io.Reader 接口的数据源
//
// 返回值：
//   - *Document: 解析后的文档对象
//   - error: 解析过程中的错误
func ParseWordDocument(r io.Reader) (*Document, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDocumentSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	return ParseWordBytes(data)
}

// ParseWordBytes 从字节数组解析 Word 文档
// 解析流程与 ParseWordFile 相同，区别在于输入源为内存中的字节数组
// 参数：
//   - data: Word 文件的原始字节数据
//
// 返回值：
//   - *Document: 解析后的文档对象
//   - error: 解析过程中的错误
func ParseWordBytes(data []byte) (*Document, error) {
	// 第一步：从字节数组中提取所有嵌入的图片
	images := extractImagesFromBytes(data)

	// 第二步：从字节数组中构建 rId 到文件名的映射表
	ridToFilename := buildRidToFilenameMapFromBytes(data)

	// 第三步：通过 wordZero 库从内存中解析文档结构
	doc, err := document.OpenFromMemory(io.NopCloser(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open document from bytes: %w", err)
	}

	// 第四步：将 wordZero 的文档结构转换为我们自己的 Document 格式
	result, err := parseWordZeroDocumentWithRidMap(doc, ridToFilename)
	if err != nil {
		return nil, err
	}

	// 将提取的图片附加到结果中
	result.Images = images
	return result, nil
}

// DocumentElement 表示文档中的一个元素（段落或表格）
// 用于保持文档中段落和表格的原始交替顺序
// 注意：Paragraph 和 Table 使用值类型而非指针，避免 append 扩容时产生悬挂指针
type DocumentElement struct {
	IsParagraph bool      // true 表示该元素是段落，false 表示是表格
	Paragraph   Paragraph // 当 IsParagraph 为 true 时，存储对应的 Paragraph（值拷贝）
	Table       Table     // 当 IsParagraph 为 false 时，存储对应的 Table（值拷贝）
}

// buildRidToFilenameMap 从文件路径构建 rId 到文件名的映射表
// 通过直接读取 ZIP 包中的关系文件（word/_rels/document.xml.rels）来获取映射
// 关系文件中记录了文档中所有外部资源的引用关系，包括图片、超链接等
// 本函数只提取类型为 image/picture 的关系
// 参数：
//   - filePath: Word 文件的路径
//
// 返回值：
//   - map[string]string: rId -> 文件名的映射，如 {"rId4": "image1.jpeg"}
func buildRidToFilenameMap(filePath string) map[string]string {
	ridToFilename := make(map[string]string)

	// 打开 ZIP 文件
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return ridToFilename
	}
	defer r.Close()

	// 遍历 ZIP 中的文件，查找关系文件
	for _, f := range r.File {
		if f.Name == "word/_rels/document.xml.rels" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}

			// 解析关系 XML 文件，提取图片关系映射
			ridToFilename = parseRelationshipsXML(string(data))
			break // 找到并解析完毕后退出循环
		}
	}

	return ridToFilename
}

// buildRidToFilenameMapFromBytes 从字节数组构建 rId 到文件名的映射表
// 功能与 buildRidToFilenameMap 相同，区别在于输入源为内存中的字节数组
// 参数：
//   - data: Word 文件的原始字节数据
//
// 返回值：
//   - map[string]string: rId -> 文件名的映射
func buildRidToFilenameMapFromBytes(data []byte) map[string]string {
	ridToFilename := make(map[string]string)

	// 从字节数组创建 ZIP Reader
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ridToFilename
	}

	// 查找并解析关系文件
	for _, f := range r.File {
		if f.Name == "word/_rels/document.xml.rels" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			xmlData, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			ridToFilename = parseRelationshipsXML(string(xmlData))
			break
		}
	}

	return ridToFilename
}

// relationshipsXML 用于反序列化 word/_rels/document.xml.rels 文件
// XML 格式示例：
//
//	<Relationships xmlns="...">
//	  <Relationship Id="rId1" Type="...image..." Target="media/image1.jpeg"/>
//	  <Relationship Id="rId2" Type="...hyperlink..." Target="http://example.com"/>
//	</Relationships>
type relationshipsXML struct {
	XMLName xml.Name           `xml:"Relationships"`
	Items   []relationshipItem `xml:"Relationship"`
}

// relationshipItem 对应单个 Relationship 元素
type relationshipItem struct {
	Id     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

// parseRelationshipsXML 解析 Word 文档的关系 XML 文件，提取 rId 到图片文件名的映射
// 使用 encoding/xml 标准库替代正则解析，可以正确处理：
//   - 任意属性排列顺序
//   - 单引号/双引号属性值
//   - 换行和缩进
//   - XML 实体转义
//
// 参数：
//   - xmlContent: 关系 XML 文件的字符串内容
//
// 返回值：
//   - map[string]string: rId -> 文件名的映射（只包含图片类型的关系）
func parseRelationshipsXML(xmlContent string) map[string]string {
	ridToFilename := make(map[string]string)

	var rels relationshipsXML
	if err := xml.Unmarshal([]byte(xmlContent), &rels); err != nil {
		return ridToFilename
	}

	for _, r := range rels.Items {
		// 只提取图片类型的关系（Type 中包含 "image" 或 "picture"）
		if strings.Contains(r.Type, "image") || strings.Contains(r.Type, "picture") {
			// 从目标路径中提取纯文件名
			// 如 "media/image1.jpeg" -> "image1.jpeg"
			filename := path.Base(r.Target)
			ridToFilename[r.Id] = filename
		}
	}

	return ridToFilename
}

// parseWordZeroDocumentWithRidMap 将 wordZero 库解析的文档结构转换为我们自己的 Document 格式
// 遍历文档中的所有元素（段落和表格），按原始顺序构建 Elements 列表
// 同时过滤掉目录（TOC）内容，并为每个段落识别标题层级
// 参数：
//   - doc: wordZero 库的文档对象
//   - ridToFilename: rId 到文件名的映射表（用于替换段落中的图片占位符）
//
// 返回值：
//   - *Document: 转换后的文档对象
//   - error: 转换过程中的错误
func parseWordZeroDocumentWithRidMap(doc *document.Document, ridToFilename map[string]string) (*Document, error) {
	result := &Document{
		Title:      detectDocumentTitle(doc, ridToFilename), // 自动检测文档标题
		Paragraphs: make([]Paragraph, 0),
		Tables:     make([]Table, 0),
		Elements:   make([]DocumentElement, 0),
		Images:     make([]Image, 0),
	}

	// 遍历文档 Body 中的所有元素（段落和表格交替出现）
	for _, elem := range doc.Body.Elements {
		switch e := elem.(type) {
		case *document.Paragraph:
			// 通过段落样式识别并跳过目录内容（TOC 段落的样式为 TOC1~TOC9 或 10~19）
			if isTocParagraph(e) {
				continue
			}

			// 提取段落文本（图片位置会被替换为占位符）
			text := getParagraphText(e, ridToFilename)

			// 识别段落的标题层级
			level := getTitleLevel(e, text)

			para := Paragraph{
				Level:   level,
				Content: text,
				IsTable: false,
			}
			// 将段落添加到 Paragraphs 列表
			result.Paragraphs = append(result.Paragraphs, para)
			// 同时在 Elements 中存储值拷贝，保持段落和表格的原始交替顺序
			// 使用值类型而非指针，避免后续 append 扩容导致指针悬挂
			result.Elements = append(result.Elements, DocumentElement{
				IsParagraph: true,
				Paragraph:   para,
			})

		case *document.Table:
			// 将表格转换为我们自己的格式
			table := Table{
				Rows: make([][]string, 0),
			}

			// 遍历表格的每一行每一列，提取单元格文本
			for rowIdx := 0; rowIdx < e.GetRowCount(); rowIdx++ {
				row := make([]string, 0)
				for colIdx := 0; colIdx < e.GetColumnCount(); colIdx++ {
					// 使用 wordZero 库的方法获取单元格文本
					cellText, err := e.GetCellText(rowIdx, colIdx)
					if err != nil {
						cellText = "" // 获取失败时使用空字符串
					}
					row = append(row, cellText)
				}
				table.Rows = append(table.Rows, row)
			}

			// 将表格添加到 Tables 列表
			result.Tables = append(result.Tables, table)
			// 同时在 Elements 中存储值拷贝，保持段落和表格的原始交替顺序
			// Table 内部的 Rows 是切片，值拷贝会共享底层数组，不会额外占用内存
			result.Elements = append(result.Elements, DocumentElement{
				IsParagraph: false,
				Table:       table,
			})
		}
	}

	return result, nil
}

// extractImagesFromZip 从 Word 文件（ZIP 格式）中提取所有嵌入的图片
// 遍历 ZIP 包中的文件，提取 word/media/ 目录下的所有文件作为图片
// 图片的 ID 使用 ZIP 中的文件名（如 "image1.jpeg"）
// 参数：
//   - filePath: Word 文件的路径
//
// 返回值：
//   - []Image: 提取到的图片列表
func extractImagesFromZip(filePath string) []Image {
	var images []Image

	r, err := zip.OpenReader(filePath)
	if err != nil {
		return images
	}
	defer r.Close()

	// 遍历 ZIP 中的文件
	for _, f := range r.File {
		// 只处理 word/media/ 目录下的文件（文档中嵌入的图片资源）
		if strings.HasPrefix(f.Name, "word/media/") {
			// 跳过超大文件，防止 OOM
			if f.UncompressedSize64 > maxImageSize {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(rc, maxImageSize))
			rc.Close()
			// 跳过读取失败或内容为空的文件
			if err != nil || len(data) == 0 {
				continue
			}
			// 提取纯文件名（去掉目录路径）
			filename := filepath.Base(f.Name)
			images = append(images, Image{
				ID:       filename, // 使用文件名作为 ID
				Filename: filename,
				Data:     data,
			})
		}
	}
	return images
}

// extractImagesFromBytes 从字节数组（ZIP 格式）中提取所有嵌入的图片
// 功能与 extractImagesFromZip 相同，区别在于输入源为内存中的字节数组
// 参数：
//   - data: Word 文件的原始字节数据
//
// 返回值：
//   - []Image: 提取到的图片列表
func extractImagesFromBytes(data []byte) []Image {
	var images []Image

	// 从字节数组创建 ZIP Reader
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return images
	}

	// 遍历 ZIP 中的文件，提取 word/media/ 目录下的图片
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			// 跳过超大文件，防止 OOM
			if f.UncompressedSize64 > maxImageSize {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			imgData, err := io.ReadAll(io.LimitReader(rc, maxImageSize))
			rc.Close()
			if err != nil || len(imgData) == 0 {
				continue
			}
			filename := filepath.Base(f.Name)
			images = append(images, Image{
				ID:       filename,
				Filename: filename,
				Data:     imgData,
			})
		}
	}
	return images
}

// isTocParagraph 通过段落样式判断是否为目录（Table of Contents）段落
// Word 自动生成的目录段落使用专用的 TOC 样式：
//   - 英文：TOC1 ~ TOC9（部分版本为 TOC 1 ~ TOC 9，带空格）
//   - 内部：纯数字 10 ~ 19（某些 Word 版本的内部 TOC 样式）
//
// 相比基于文本内容匹配的方式（isTocContent），基于样式名的判断：
//   - 不会误杀包含 "toc"、"hyperlink" 等关键词的正常段落
//   - 精确识别 Word 标记的目录段落
//
// 参数：
//   - para: wordZero 的段落对象
//
// 返回值：
//   - bool: 如果是目录段落返回 true
func isTocParagraph(para *document.Paragraph) bool {
	if para.Properties == nil || para.Properties.ParagraphStyle == nil {
		return false
	}

	styleName := strings.ToLower(para.Properties.ParagraphStyle.Val)

	// 匹配 TOC1 ~ TOC9（可能有空格，如 "TOC 1"）
	if len(styleName) >= 4 && strings.HasPrefix(styleName, "toc") {
		suffix := strings.TrimSpace(styleName[3:])
		if n, err := strconv.Atoi(suffix); err == nil && n >= 1 && n <= 9 {
			return true
		}
	}

	// 匹配 Word 内部 TOC 样式：纯数字 10 ~ 19
	if n, err := strconv.Atoi(styleName); err == nil && n >= 10 && n <= 19 {
		return true
	}

	return false
}

// getTitleLevel 综合判断一个段落的标题层级
// 判断优先级从高到低：
//  1. 段落样式（如 "Heading 1"、"标题 2"）——最可靠的依据
//  2. 字号 + 加粗（字号在 titleFontList 定义的范围内且文字加粗）
//  3. Markdown 风格标题（文本以 # 开头）
//  4. 加粗的数字编号段落（如 "1. 审核目标"）——推测为二级标题
//
// 参数：
//   - para: wordZero 的段落对象
//   - text: 段落的文本内容
//
// 返回值：
//   - TitleLevel: 识别到的标题层级
func getTitleLevel(para *document.Paragraph, text string) TitleLevel {
	// 优先级 1：检查段落样式名称
	if para.Properties != nil && para.Properties.ParagraphStyle != nil {
		styleName := para.Properties.ParagraphStyle.Val
		if level := getTitleLevelFromStyle(styleName); level != NotATitle {
			return level
		}
	}

	// 优先级 2-4：当段落包含 Run 元素时进一步分析
	if len(para.Runs) > 0 {
		// 优先级 2：通过字号 + 加粗判断
		// 只检查第一个有字号属性的 Run
		for _, run := range para.Runs {
			if run.Properties != nil && run.Properties.FontSize != nil {
				fontSizeStr := run.Properties.FontSize.Val
				isBold := run.Properties.Bold != nil
				if level := getTitleLevelFromFontSize(fontSizeStr, isBold); level != NotATitle {
					return level
				}
				break // 只检查第一个 Run 的字号
			}
		}

		// 检查段落中是否有任何 Run 是加粗的（用于后续判断）
		isBold := false
		for _, run := range para.Runs {
			if run.Properties != nil && run.Properties.Bold != nil {
				isBold = true
				break
			}
		}

		// 优先级 3：检查 Markdown 风格标题（文本以 # 开头）
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "#") {
			count := 0
			for _, c := range text {
				if c == '#' {
					count++
				} else if c == ' ' || c == '\t' {
					break // 遇到空格或制表符时停止计数
				} else {
					count = 0 // 遇到其他字符说明不是 Markdown 标题
					break
				}
			}
			if count >= 1 && count <= 6 {
				return TitleLevel(count)
			}
		}

		// 优先级 4：加粗的数字编号段落推测为二级标题
		// 如 "1. 审核目标"、"2. 审核范围" 等加粗文本
		// 限定文本长度不超过 100 字符，避免将长段落误判为标题
		if isBold && len(text) > 0 && len(text) < 100 {
			if reNumberedParagraph.MatchString(text) {
				return Level2 // 统一视为二级标题
			}
		}
	}

	return NotATitle
}

// detectDocumentTitle 自动检测文档的标题
// 检测策略：扫描文档前 10 个段落，查找第一个满足以下条件的段落：
//  1. 文本内容不为空
//  2. 段落居中对齐（w:jc val="center"）
//  3. 具有标题视觉特征（标题样式 / 加粗 / 大字号 >= 18pt）
//
// 参数：
//   - doc: wordZero 的文档对象
//   - ridToFilename: rId 到文件名的映射表
//
// 返回值：
//   - string: 检测到的标题文本，如果未找到则返回空字符串
func detectDocumentTitle(doc *document.Document, ridToFilename map[string]string) string {
	maxScan := 10 // 最多扫描前 10 个段落
	scanned := 0

	for _, elem := range doc.Body.Elements {
		if scanned >= maxScan {
			break
		}
		// 只处理段落元素，跳过表格等
		para, ok := elem.(*document.Paragraph)
		if !ok {
			continue
		}
		scanned++

		// 跳过 TOC 目录段落，避免将 "目录" 误识别为文档标题
		if isTocParagraph(para) {
			continue
		}

		text := strings.TrimSpace(getParagraphText(para, ridToFilename))
		// 跳过空段落
		if text == "" {
			continue
		}

		// 必须居中对齐
		if !isCentered(para) {
			continue
		}

		// 必须具有标题视觉特征
		if hasTitleCharacteristics(para, text) {
			return text
		}
	}
	return ""
}

// isCentered 检查段落是否为居中对齐
// 在 Word XML 中，居中对齐表示为 <w:jc w:val="center"/>
// 参数：
//   - para: wordZero 的段落对象
//
// 返回值：
//   - bool: 如果段落居中对齐返回 true
func isCentered(para *document.Paragraph) bool {
	if para.Properties == nil || para.Properties.Justification == nil {
		return false
	}
	return para.Properties.Justification.Val == "center"
}

// hasTitleCharacteristics 检查段落是否具有标题的视觉特征
// 满足以下任一条件即认为具有标题特征：
//   - 段落样式为标题样式（Heading 1-6、标题 1-6 等）
//   - 文字加粗（Bold）
//   - 字号较大（>= 18pt，即半磅值 >= 36）
//
// 参数：
//   - para: wordZero 的段落对象
//   - text: 段落的文本内容（当前未使用，保留扩展）
//
// 返回值：
//   - bool: 如果具有标题特征返回 true
func hasTitleCharacteristics(para *document.Paragraph, text string) bool {
	// 检查段落样式是否为标题样式
	if para.Properties != nil && para.Properties.ParagraphStyle != nil {
		styleName := para.Properties.ParagraphStyle.Val
		if level := getTitleLevelFromStyle(styleName); level != NotATitle {
			return true
		}
	}

	// 检查 Run 的格式属性
	for _, run := range para.Runs {
		if run.Properties == nil {
			continue
		}

		// 检查加粗
		if run.Properties.Bold != nil {
			return true
		}

		// 检查大字号（>= 18pt，Word 中 18pt = 36 半磅）
		if run.Properties.FontSize != nil && run.Properties.FontSize.Val != "" {
			fontSize, err := strconv.Atoi(run.Properties.FontSize.Val)
			if err == nil && fontSize >= 36 {
				return true
			}
		}
	}

	return false
}

// ParseFromZip 从 ZIP 文件打开 Word 文档
// 是 ParseWordFile 的别名函数，提供向后兼容性
// 参数：
//   - filePath: Word 文件路径
//
// 返回值：
//   - *Document: 解析后的文档对象
//   - error: 解析过程中的错误
func ParseFromZip(filePath string) (*Document, error) {
	return ParseWordFile(filePath)
}

// SaveImages 将文档中提取的所有图片保存到指定目录
// 支持两种文件命名方式：
//  1. 原始文件名（默认）
//  2. UUID 唯一文件名（避免重名冲突）
//
// 文件名前缀可以自定义（如 "doc123_"）
//
// 注意：useUniqueName 和 prefix 的组合顺序是先生成 UUID 文件名，再添加前缀
// 即最终文件名格式为：{prefix}{uuid}{ext} 或 {prefix}{original_name}
//
// 参数：
//   - outputDir: 图片保存的输出目录路径
//   - prefix: 文件名前缀，为空则不添加前缀
//   - useUniqueName: 是否使用 UUID 作为文件名（避免文件名冲突）
//
// 返回值：
//   - error: 保存过程中的错误
func (d *Document) SaveImages(outputDir string, prefix string, useUniqueName bool) error {
	if len(d.Images) == 0 {
		return nil
	}

	// 确保输出目录存在
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// 获取输出目录的绝对路径，用于后续路径穿越校验
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	for i, img := range d.Images {
		// 从原始文件名中提取纯文件名部分，去除任何目录路径
		filename := filepath.Base(img.Filename)
		// 先应用唯一文件名（UUID），再应用前缀
		if useUniqueName {
			uid := generateUUID()
			ext := getFileExt(filename)
			filename = fmt.Sprintf("%s%s", uid, ext)
		}
		if prefix != "" {
			// 前缀也只取基本名称部分，防止 prefix 中包含路径分隔符
			filename = filepath.Base(prefix) + filename
		}

		// 记录实际保存的文件名，供后续 Markdown 图片路径替换使用
		d.Images[i].SavedFilename = filename

		// 拼接完整文件路径并写入磁盘
		fullPath := filepath.Join(absOutputDir, filename)

		// 安全校验：确保最终路径仍在输出目录内，防止路径穿越攻击
		if !strings.HasPrefix(fullPath, absOutputDir+string(os.PathSeparator)) {
			return fmt.Errorf("invalid image filename (path traversal detected): %s", img.Filename)
		}

		if err := os.WriteFile(fullPath, img.Data, 0644); err != nil {
			return fmt.Errorf("failed to write image %d: %w", i, err)
		}
	}

	return nil
}

// GetImageMarkdown 将内容中的图片占位符（{{IMAGE:filename}}）替换为 Markdown 图片语法
// 使用 Image.ID（文件名）作为查找键来匹配占位符中的文件名
// 参数：
//   - content: 包含图片占位符的文本内容
//
// 返回值：
//   - string: 替换后的 Markdown 文本，图片格式为 ![filename](filename)
func (d *Document) GetImageMarkdown(content string) string {
	// 构建文件名到自身的映射（用于占位符替换）
	ridToFilename := make(map[string]string)
	for _, img := range d.Images {
		ridToFilename[img.ID] = img.Filename
	}

	// 匹配 {{IMAGE:xxx}} 格式的占位符并替换为 Markdown 图片语法
	return reImagePlaceholder.ReplaceAllStringFunc(content, func(match string) string {
		parts := reImagePlaceholder.FindStringSubmatch(match)
		if len(parts) >= 2 {
			rid := parts[1]
			if filename, ok := ridToFilename[rid]; ok {
				return fmt.Sprintf("![](%s)", filename)
			}
		}
		return match // 未找到对应图片，保留原始占位符
	})
}

// GetImageMarkdownWithPrefix 将内容中的图片占位符替换为带前缀/UUID 的 Markdown 图片路径
// 优先使用 SaveImages 方法已设置的 SavedFilename
// 如果尚未调用 SaveImages，则根据参数动态生成文件名
//
// 文件名生成逻辑（与 SaveImages 保持一致）：
//  1. 如果 SavedFilename 已设置 -> 直接使用
//  2. 如果 useUniqueName 为 true -> 先生成 UUID 文件名
//  3. 如果 prefix 不为空 -> 再添加前缀
//
// 参数：
//   - content: 包含图片占位符的文本内容
//   - prefix: 文件名前缀
//   - useUniqueName: 是否使用 UUID 文件名
//
// 返回值：
//   - string: 替换后的 Markdown 文本
func (d *Document) GetImageMarkdownWithPrefix(content string, prefix string, useUniqueName bool) string {
	ridToFilename := make(map[string]string)
	for _, img := range d.Images {
		// 优先使用 SaveImages 已设置的文件名
		filename := img.SavedFilename
		if filename == "" {
			// 未调用过 SaveImages，根据参数生成文件名（逻辑与 SaveImages 一致）
			filename = filepath.Base(img.Filename)
			if useUniqueName {
				uid := generateUUID()
				ext := getFileExt(filename)
				filename = fmt.Sprintf("%s%s", uid, ext)
			}
			if prefix != "" {
				filename = filepath.Base(prefix) + filename
			}
		}
		ridToFilename[img.ID] = filename
	}

	// 替换占位符
	return reImagePlaceholder.ReplaceAllStringFunc(content, func(match string) string {
		parts := reImagePlaceholder.FindStringSubmatch(match)
		if len(parts) >= 2 {
			rid := parts[1]
			if filename, ok := ridToFilename[rid]; ok {
				return fmt.Sprintf("![](%s)", filename)
			}
		}
		return match
	})
}

// generateUUID 生成一个随机的 32 字符十六进制字符串（128 位随机数）
// 注意：这不是标准的 UUID 格式（没有连字符），而是 32 个十六进制字符
func generateUUID() string {
	u := uuid.New()
	return strings.ReplaceAll(u.String(), "-", "")[:32]
}

// getFileExt 从文件名中提取扩展名（包含点号）
// 如 "image1.jpeg" -> ".jpeg"
// 如果文件名中没有扩展名，返回空字符串
// 参数：
//   - filename: 文件名
//
// 返回值：
//   - string: 文件扩展名（包含点号），如 ".jpeg"、".png"
func getFileExt(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return filename[i:]
		}
	}
	return ""
}

// ToMarkdown 将整个文档转换为 Markdown 格式
// 段落和表格保持文档中的原始顺序
// 标题段落会根据层级添加对应数量的 # 前缀
// 普通段落直接输出文本内容
// 表格转换为 Markdown 表格格式
// 返回值：
//   - string: Markdown 格式的文档内容
func (d *Document) ToMarkdown() string {
	var buf bytes.Buffer

	// 优先使用 Elements 列表（保持段落和表格的交替顺序）
	if len(d.Elements) > 0 {
		for _, elem := range d.Elements {
			if elem.IsParagraph {
				para := elem.Paragraph
				content := para.Content
				if para.Level > NotATitle {
					// 标题段落：添加对应数量的 # 前缀
					markers := strings.Repeat("#", int(para.Level))
					buf.WriteString(fmt.Sprintf("%s %s\n\n", markers, content))
				} else {
					// 普通段落：直接输出
					buf.WriteString(content)
					buf.WriteString("\n\n")
				}
			} else {
				// 表格：调用 Table 的 ToMarkdown 方法
				buf.WriteString(elem.Table.ToMarkdown())
				buf.WriteString("\n")
			}
		}
	} else {
		// 降级处理：如果 Elements 为空（理论上不会出现），则分别输出段落和表格
		// 这种情况下段落全部排在表格前面，可能丢失原始的段落-表格交替顺序
		for _, para := range d.Paragraphs {
			content := para.Content
			if para.Level > NotATitle {
				markers := strings.Repeat("#", int(para.Level))
				buf.WriteString(fmt.Sprintf("%s %s\n\n", markers, content))
			} else {
				buf.WriteString(content)
				buf.WriteString("\n\n")
			}
		}

		// 追加所有表格
		for _, table := range d.Tables {
			buf.WriteString(table.ToMarkdown())
			buf.WriteString("\n")
		}
	}

	return buf.String()
}

// ToMarkdown 将表格转换为 Markdown 表格格式
// 第一行作为表头，第二行为分隔线（---）
// 单元格内的 | 字符会被转义为 \| 以避免破坏表格结构
// 如果各行列数不一致，以第一行的列数为基准进行补齐
// 格式示例：
//
//	| 列1 | 列2 | 列3 |
//	| --- | --- | --- |
//	| 数据1 | 数据2 | 数据3 |
//
// 返回值：
//   - string: Markdown 格式的表格字符串
func (t *Table) ToMarkdown() string {
	if len(t.Rows) == 0 {
		return ""
	}

	var buf bytes.Buffer

	// 以第一行的列数为基准列数
	colCount := len(t.Rows[0])
	if colCount == 0 {
		return ""
	}

	for i, row := range t.Rows {
		// 补齐列数不一致的行
		paddedRow := row
		for len(paddedRow) < colCount {
			paddedRow = append(paddedRow, "")
		}

		// 转义单元格中的 | 字符，防止破坏 Markdown 表格结构
		escaped := make([]string, colCount)
		for j, cell := range paddedRow {
			escaped[j] = strings.ReplaceAll(cell, "|", `\|`)
		}

		// 写入一行：| 单元格1 | 单元格2 | ... |
		buf.WriteString("| ")
		buf.WriteString(strings.Join(escaped, " | "))
		buf.WriteString(" |\n")

		// 在第一行（表头）后添加分隔线
		if i == 0 {
			buf.WriteString("| ")
			for j := 0; j < colCount; j++ {
				if j > 0 {
					buf.WriteString(" | ")
				}
				buf.WriteString("---")
			}
			buf.WriteString(" |\n")
		}
	}

	return buf.String()
}
