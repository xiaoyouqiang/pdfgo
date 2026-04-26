package font

import "github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"

// SimpleFontDecoder 处理 Type1、TrueType 和 Type3 简单字体。
// 简单字体使用单字节编码（每个字符 1 字节），通过以下优先级解码：
//  1. ToUnicode CMap（最准确，PDF 内嵌的字符映射表）
//  2. 自定义编码表（字体字典中的 Encoding 条目）
//  3. WinAnsiEncoding（CP1252，最常见的默认编码）
type SimpleFontDecoder struct {
	name      string         // 字体名称
	toUnicode *CMap          // ToUnicode CMap 映射表（可选）
	encoding  map[byte]rune  // 自定义编码表（可选）
	widths    map[byte]float64 // 字符宽度表（字节 → 宽度比例）
	fontType  string         // 字体类型："Type1"、"TrueType"、"Type3"
}

// NewSimpleFontDecoder 创建一个简单字体解码器。
//   - name: 字体名称
//   - toUnicode: ToUnicode CMap（可为 nil）
//   - encoding: 自定义编码表（可为 nil）
//   - widths: 字符宽度映射（可为 nil）
//   - fontType: 字体类型
func NewSimpleFontDecoder(name string, toUnicode *CMap, encoding map[byte]rune, widths map[byte]float64, fontType string) *SimpleFontDecoder {
	return &SimpleFontDecoder{
		name:      name,
		toUnicode: toUnicode,
		encoding:  encoding,
		widths:    widths,
		fontType:  fontType,
	}
}

// Decode 将字节切片解码为 Unicode 字符和对应的宽度值。
// 解码优先级：ToUnicode CMap → 自定义编码 → WinAnsiEncoding。
func (d *SimpleFontDecoder) Decode(data []byte) ([]rune, []float64) {
	runes := make([]rune, 0, len(data))
	widths := make([]float64, 0, len(data))
	for _, b := range data {
		var r rune
		// 按优先级尝试解码
		if d.toUnicode != nil {
			// 最高优先级：使用 ToUnicode CMap
			r = d.toUnicode.DecodeSingle(int(b))
			if r == 0 {
				r = '?' // 无法映射的字符用问号替代
			}
		} else if d.encoding != nil {
			// 次优先级：使用自定义编码表
			if mapped, ok := d.encoding[b]; ok {
				r = mapped
			} else {
				r = rune(b)
			}
		} else {
			// 最低优先级：使用 WinAnsiEncoding（CP1252）
			r = WinAnsiEncoding[b]
			if r == 0 {
				r = rune(b)
			}
		}
		runes = append(runes, r)

		// 查找字符宽度，未定义时使用默认值 0.5
		if w, ok := d.widths[b]; ok {
			widths = append(widths, w)
		} else {
			widths = append(widths, 0.5)
		}
	}
	return runes, widths
}

// FontName 返回字体名称
func (d *SimpleFontDecoder) FontName() string {
	return d.name
}

// FontInfo 返回字体的元信息
func (d *SimpleFontDecoder) FontInfo() model.FontInfo {
	return model.FontInfo{
		Name:     d.name,
		FontType: d.fontType,
	}
}
