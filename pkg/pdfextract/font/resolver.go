// Package font 提供 PDF 字体解码功能。
//
// PDF 中的文本以编码字节的形式存储，需要通过字体解码器将其转换为 Unicode 字符。
// 本包支持以下字体类型：
//   - Type1/TrueType/Type3（简单字体）：单字节编码，通过 CMap 或编码表映射
//   - Type0/CID（复合字体）：多字节编码，通过 ToUnicode CMap 映射
package font

// FontDecoder 是字体解码器接口，将 PDF 字节编码转换为 Unicode 字符。
// 每种字体类型（Type1、TrueType、Type0/CID 等）有自己的实现。
type FontDecoder interface {
	// Decode 将字节切片解码为 Unicode 字符切片和对应的宽度值
	Decode(data []byte) ([]rune, []float64)
	// FontName 返回字体名称
	FontName() string
}

// FontResolver 管理字体名称到解码器的映射关系。
// PDF 页面资源中定义了字体名称（如 "F1"、"F2"），
// FontResolver 将这些名称关联到对应的解码器实例。
type FontResolver struct {
	fonts map[string]FontDecoder // 字体名称 → 解码器映射
}

// NewFontResolver 创建一个新的字体解析器
func NewFontResolver() *FontResolver {
	return &FontResolver{
		fonts: make(map[string]FontDecoder),
	}
}

// Resolve 根据字体名称查找对应的解码器
func (r *FontResolver) Resolve(name string) (FontDecoder, bool) {
	if f, ok := r.fonts[name]; ok {
		return f, true
	}
	return nil, false
}

// Register 注册一个字体解码器
func (r *FontResolver) Register(name string, decoder FontDecoder) {
	r.fonts[name] = decoder
}

// AllFonts 返回所有已注册的字体映射表
func (r *FontResolver) AllFonts() map[string]FontDecoder {
	return r.fonts
}
