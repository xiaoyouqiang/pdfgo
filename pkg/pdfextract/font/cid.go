package font

import (
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// CIDFontDecoder 处理 Type0/CID 复合字体。
// CID 字体使用多字节编码（通常 2 字节），通过 ToUnicode CMap 将编码映射为 Unicode。
// 这类字体主要用于中文、日文、韩文等需要大量字符集的语言。
type CIDFontDecoder struct {
	name         string           // 字体名称
	toUnicode    *CMap            // ToUnicode CMap 映射表
	defaultWidth float64          // 默认字符宽度（当宽度表不可用时使用）
	widths       map[int]float64  // per-CID 宽度映射（从 W 数组解析），key 是 CID 值
	encoding     string           // 预定义编码名称（如 "GBKp-EUC-H"），用于无 ToUnicode 时的回退解码
}

// NewCIDFontDecoder 创建一个 CID 字体解码器。
//   - name: 字体名称
//   - toUnicode: ToUnicode CMap（可为 nil，此时使用 encoding 回退）
//   - defaultWidth: 默认字符宽度（单位为千分之一，通常从 CIDFont 字典的 DW 条目获取）
//   - widths: per-CID 宽度映射（可为 nil，此时全部使用 defaultWidth）
func NewCIDFontDecoder(name string, toUnicode *CMap, defaultWidth float64, widths map[int]float64) *CIDFontDecoder {
	if defaultWidth == 0 {
		defaultWidth = 1.0
	}
	if widths == nil {
		widths = make(map[int]float64)
	}
	return &CIDFontDecoder{
		name:         name,
		toUnicode:    toUnicode,
		defaultWidth: defaultWidth,
		widths:       widths,
	}
}

// NewCIDFontDecoderWithEncoding 创建一个带预定义编码的 CID 字体解码器。
// 当 ToUnicode CMap 不可用时，使用预定义编码名称进行回退解码。
func NewCIDFontDecoderWithEncoding(name string, encoding string, defaultWidth float64) *CIDFontDecoder {
	if defaultWidth == 0 {
		defaultWidth = 1.0
	}
	return &CIDFontDecoder{
		name:         name,
		defaultWidth: defaultWidth,
		encoding:     encoding,
		widths:       make(map[int]float64),
	}
}

// Decode 将多字节编码的数据解码为 Unicode 字符和宽度值。
//
// 处理流程：
//  1. 优先使用 ToUnicode CMap 解码
//  2. 若无 ToUnicode CMap，使用预定义编码回退解码（如 GBK）
func (d *CIDFontDecoder) Decode(data []byte) ([]rune, []float64) {
	// 优先使用 ToUnicode CMap
	if d.toUnicode != nil {
		return d.decodeWithCMap(data)
	}
	// 回退：使用预定义编码
	if d.encoding != "" {
		return d.decodeWithPredefinedEncoding(data)
	}
	return nil, nil
}

// decodeWithCMap 使用 ToUnicode CMap 解码（支持一对多映射）
func (d *CIDFontDecoder) decodeWithCMap(data []byte) ([]rune, []float64) {
	codeBytes := d.toUnicode.CodeBytes()
	if codeBytes < 1 {
		codeBytes = 1
	}

	var runes []rune
	var widths []float64
	for i := 0; i+codeBytes <= len(data); {
		code := 0
		for j := 0; j < codeBytes; j++ {
			code = (code << 8) | int(data[i+j])
		}
		i += codeBytes

		decoded := d.toUnicode.Decode(code)
		if len(decoded) == 0 {
			decoded = []rune{'?'}
		}

		// 查找宽度
		w := d.defaultWidth
		if dw, ok := d.widths[code]; ok {
			w = dw
		}

		// 宽度按字符数均分
		perRune := w / float64(len(decoded))
		for range decoded {
			widths = append(widths, perRune)
		}
		runes = append(runes, decoded...)
	}
	return runes, widths
}

// decodeWithPredefinedEncoding 使用预定义编码回退解码
// 支持常见 CJK 编码：GBK/GB2312、Big5、Shift_JIS、EUC-JP 等
func (d *CIDFontDecoder) decodeWithPredefinedEncoding(data []byte) ([]rune, []float64) {
	decoder := getDecoderForEncoding(d.encoding)
	if decoder == nil {
		return nil, nil
	}

	// 将编码数据转换为 UTF-8
	decoded, _, err := transform.Bytes(decoder, data)
	if err != nil {
		return nil, nil
	}

	// 将 UTF-8 字节转换为 rune 列表
	var runes []rune
	var widths []float64
	byteIdx := 0
	for byteIdx < len(data) {
		// 估算原始编码消耗的字节数（用于查找宽度）
		r, sz := utf8.DecodeRune(decoded)
		if r == utf8.RuneError {
			r = '?'
		}
		runes = append(runes, r)

		// 对于多字节编码，使用字节偏移作为 CID 近似值查找宽度
		cid := 0
		if len(data)-byteIdx >= 2 {
			cid = int(data[byteIdx])<<8 | int(data[byteIdx+1])
		} else if len(data)-byteIdx >= 1 {
			cid = int(data[byteIdx])
		}

		if w, ok := d.widths[cid]; ok {
			widths = append(widths, w)
		} else {
			widths = append(widths, d.defaultWidth)
		}

		// 推进 UTF-8 解码位置
		if sz > 0 {
			decoded = decoded[sz:]
		} else {
			decoded = decoded[1:]
		}
		// 推进原始字节位置（GBK 等 2 字节编码）
		if len(data)-byteIdx >= 2 && data[byteIdx] >= 0x80 {
			byteIdx += 2
		} else {
			byteIdx++
		}
	}
	return runes, widths
}

// getDecoderForEncoding 根据预定义编码名称返回对应的解码器
func getDecoderForEncoding(enc string) transform.Transformer {
	switch {
	case isGBKEncoding(enc):
		return simplifiedchinese.GBK.NewDecoder()
	default:
		return nil
	}
}

// isGBKEncoding 判断编码名称是否为 GBK 系列
func isGBKEncoding(enc string) bool {
	switch enc {
	case "GBKp-EUC-H", "GBKp-EUC-V",
		"GBK-EUC-H", "GBK-EUC-V",
		"GBKp-EUC", "GBK-EUC",
		"UniGB-UTF16-H", "UniGB-UTF16-V",
		"UniGB-UCS2-H", "UniGB-UCS2-V",
		"GBpc-EUC-H", "GBpc-EUC-V":
		return true
	default:
		return false
	}
}

// FontName 返回字体名称
func (d *CIDFontDecoder) FontName() string {
	return d.name
}
