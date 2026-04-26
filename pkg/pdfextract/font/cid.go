package font

// CIDFontDecoder 处理 Type0/CID 复合字体。
// CID 字体使用多字节编码（通常 2 字节），通过 ToUnicode CMap 将编码映射为 Unicode。
// 这类字体主要用于中文、日文、韩文等需要大量字符集的语言。
type CIDFontDecoder struct {
	name         string  // 字体名称
	toUnicode    *CMap   // ToUnicode CMap 映射表
	defaultWidth float64 // 默认字符宽度（当宽度表不可用时使用）
}

// NewCIDFontDecoder 创建一个 CID 字体解码器。
//   - name: 字体名称
//   - toUnicode: ToUnicode CMap（必需，否则无法解码）
//   - defaultWidth: 默认字符宽度（单位为千分之一，通常从 CIDFont 字典的 DW 条目获取）
func NewCIDFontDecoder(name string, toUnicode *CMap, defaultWidth float64) *CIDFontDecoder {
	if defaultWidth == 0 {
		defaultWidth = 1.0
	}
	return &CIDFontDecoder{
		name:         name,
		toUnicode:    toUnicode,
		defaultWidth: defaultWidth,
	}
}

// Decode 将多字节编码的数据解码为 Unicode 字符和宽度值。
//
// 处理流程：
//  1. 确定每个编码的字节数（通过 CMap 的 CodeBytes 方法）
//  2. 每次读取 codeBytes 个字节，组合为编码值
//  3. 使用 CMap 将编码值映射为 Unicode 字符
//  4. 使用默认宽度作为字符宽度
func (d *CIDFontDecoder) Decode(data []byte) ([]rune, []float64) {
	if d.toUnicode == nil {
		return nil, nil
	}
	// 获取每个编码的字节数
	codeBytes := d.toUnicode.CodeBytes()
	if codeBytes < 1 {
		codeBytes = 1
	}

	var runes []rune
	var widths []float64
	for i := 0; i+codeBytes <= len(data); {
		// 将 codeBytes 个字节组合为一个整数编码
		code := 0
		for j := 0; j < codeBytes; j++ {
			code = (code << 8) | int(data[i+j])
		}
		i += codeBytes

		// 通过 CMap 解码为 Unicode 字符
		r := d.toUnicode.DecodeSingle(code)
		if r == 0 {
			r = '?' // 无法映射的字符用问号替代
		}
		runes = append(runes, r)
		widths = append(widths, d.defaultWidth)
	}
	return runes, widths
}

// FontName 返回字体名称
func (d *CIDFontDecoder) FontName() string {
	return d.name
}
