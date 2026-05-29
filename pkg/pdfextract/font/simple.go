package font

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
// 支持 CMap 中的一对多映射（如连字符 "fi" 展开为 'f' 和 'i'），
// 宽度按字符数均分以保证总位移不变。
func (d *SimpleFontDecoder) Decode(data []byte) ([]rune, []float64) {
	var runes []rune
	var widths []float64
	for _, b := range data {
		var decoded []rune
		// 按优先级尝试解码
		if d.toUnicode != nil {
			// 最高优先级：使用 ToUnicode CMap（支持一对多映射）
			decoded = d.toUnicode.Decode(int(b))
			if len(decoded) == 0 {
				decoded = []rune{'?'}
			}
		} else if d.encoding != nil {
			// 次优先级：使用自定义编码表
			if mapped, ok := d.encoding[b]; ok {
				decoded = []rune{mapped}
			} else {
				decoded = []rune{rune(b)}
			}
		} else {
			// 最低优先级：使用 WinAnsiEncoding（CP1252）
			r := WinAnsiEncoding[b]
			if r == 0 {
				r = rune(b)
			}
			decoded = []rune{r}
		}

		// 查找字符宽度，未定义时使用默认值 0.5
		w := 0.5
		if dw, ok := d.widths[b]; ok {
			w = dw
		}

		// 宽度按解码字符数均分（连字符展开后每个字符各占一部分）
		perRune := w / float64(len(decoded))
		for range decoded {
			widths = append(widths, perRune)
		}
		runes = append(runes, decoded...)
	}
	return runes, widths
}

// FontName 返回字体名称
func (d *SimpleFontDecoder) FontName() string {
	return d.name
}

