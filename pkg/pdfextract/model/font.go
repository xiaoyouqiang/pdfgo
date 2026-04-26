// Package model 定义了 PDF 提取过程中使用的所有数据结构。
// 包括字符、文本行、文本框、表格单元格、表格、图片等核心类型。
package model

// FontInfo 描述一个字符的字体信息。
// 从 PDF 内容流的 Tf 操作符和字体字典中提取。
type FontInfo struct {
	Name     string     // 字体名称，如 "SimSun"、"Helvetica"
	Size     float64    // 字体大小（单位：磅 pt）
	Color    [3]float64 // 文字颜色，RGB 格式，每个分量范围为 [0-1]
	Bold     bool       // 是否为粗体
	Italic   bool       // 是否为斜体
	FontType string     // 字体类型："Type1"、"TrueType"、"Type0"、"Type3"
}
