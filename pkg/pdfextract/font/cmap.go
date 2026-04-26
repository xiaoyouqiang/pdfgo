package font

import (
	"errors"
	"regexp"
	"strconv"
)

// CMap 实现 PDF ToUnicode CMap 的解析和解码。
// CMap（Character Map）是 PDF 中用于将字符编码映射为 Unicode 码点的查找表。
//
// CMap 包含两种映射类型：
//   - bfchar：单个字符映射，如 <01> <0041>（编码 0x01 → Unicode 'A'）
//   - bfrange：范围映射，如 <00> <FF> <4E00>（编码 0x00~0xFF → Unicode 0x4E00 起）
type CMap struct {
	singleMappings map[int]rune    // bfchar 单个映射：源编码 → Unicode 字符
	rangeMappings  []RangeMapping  // bfrange 范围映射列表
	codeLength     int             // 十六进制编码的字符长度（用于确定多字节编码的字节数）
}

// RangeMapping 表示一个 bfrange 映射条目。
// 将连续的编码范围 [Start, End] 映射到从 Base 开始的连续 Unicode 码点。
type RangeMapping struct {
	Start int // 范围起始编码
	End   int // 范围结束编码
	Base  int // Unicode 基准码点
}

// 用于匹配 CMap 定义中各种标记的正则表达式
var (
	beginbfcharRx  = regexp.MustCompile(`beginbfchar\s*`)   // bfchar 段开始
	endbfcharRx    = regexp.MustCompile(`endbfchar\s*`)     // bfchar 段结束
	beginbfrangeRx = regexp.MustCompile(`beginbfrange\s*`)  // bfrange 段开始
	endbfrangeRx   = regexp.MustCompile(`endbfrange\s*`)    // bfrange 段结束
	hexPairRx      = regexp.MustCompile(`<([0-9A-Fa-f]+)>`) // 十六进制值匹配
)

// ParseCMap 从原始字节解析 CMap 映射表。
// 依次解析 codespace、bfchar 和 bfrange 三个部分。
func ParseCMap(data []byte) (*CMap, error) {
	cmap := &CMap{
		singleMappings: make(map[int]rune),
		codeLength:     1,
	}

	// 移除注释
	data = stripComments(data)

	// 解析 bfchar 段（单个字符映射）
	if err := parsebfchar(data, cmap); err != nil {
		return nil, err
	}

	// 解析 bfrange 段（范围映射）
	if err := parsebfrange(data, cmap); err != nil {
		return nil, err
	}

	// 解析 codespace 段（确定编码长度）
	parseCodespace(data, cmap)

	return cmap, nil
}

// stripComments 移除 CMap 中的注释行（以 % 开头的行）
func stripComments(data []byte) []byte {
	var out []byte
	for i := 0; i < len(data); i++ {
		if data[i] == '%' {
			for i < len(data) && data[i] != 10 && data[i] != 13 {
				i++
			}
			continue
		}
		out = append(out, data[i])
	}
	return out
}

// parsebfchar 解析所有 beginbfchar...endbfchar 段。
// bfchar 段的格式：每行两个十六进制值，<源编码> <Unicode码点>
// 例如：<01> <0041> 表示编码 0x01 映射到 Unicode 'A'
func parsebfchar(data []byte, cmap *CMap) error {
	offset := 0
	for offset < len(data) {
		m := beginbfcharRx.FindIndex(data[offset:])
		if m == nil {
			break
		}
		start := offset + m[1]
		m2 := endbfcharRx.FindIndex(data[start:])
		if m2 == nil {
			return errors.New("beginbfchar without endbfchar")
		}
		end := start + m2[0]
		segment := data[start:end]
		// 成对提取十六进制值
		pairs := hexPairRx.FindAllSubmatch(segment, -1)
		for i := 0; i+1 < len(pairs); i += 2 {
			src := parseHex(string(pairs[i][1]))   // 源编码
			dst := parseHex(string(pairs[i+1][1])) // 目标 Unicode 码点
			if src < 0 || dst < 0 {
				continue
			}
			cmap.singleMappings[src] = rune(dst)
			// 记录最长的十六进制编码长度
			hexLen := len(string(pairs[i][1]))
			if hexLen > cmap.codeLength {
				cmap.codeLength = hexLen
			}
		}
		offset = start + m2[1]
	}
	return nil
}

// parsebfrange 解析所有 beginbfrange...endbfrange 段。
// bfrange 段的格式：每行三个十六进制值，<起始编码> <结束编码> <基准Unicode>
// 例如：<00> <FF> <4E00> 表示编码 0x00~0xFF 映射到 Unicode 0x4E00~0x4EFF
func parsebfrange(data []byte, cmap *CMap) error {
	offset := 0
	for offset < len(data) {
		m := beginbfrangeRx.FindIndex(data[offset:])
		if m == nil {
			break
		}
		start := offset + m[1]
		m2 := endbfrangeRx.FindIndex(data[start:])
		if m2 == nil {
			return errors.New("beginbfrange without endbfrange")
		}
		end := start + m2[0]
		segment := data[start:end]

		lines := splitLines(segment)
		for _, line := range lines {
			pairs := hexPairRx.FindAllSubmatch(line, -1)
			if len(pairs) < 3 {
				continue
			}
			startCode := parseHex(string(pairs[0][1])) // 范围起始
			endCode := parseHex(string(pairs[1][1]))   // 范围结束
			base := parseHex(string(pairs[2][1]))      // Unicode 基准
			if startCode < 0 || endCode < 0 || base < 0 || startCode > endCode {
				continue
			}
			cmap.rangeMappings = append(cmap.rangeMappings, RangeMapping{
				Start: startCode,
				End:   endCode,
				Base:  base,
			})
			hexLen := len(string(pairs[0][1]))
			if hexLen > cmap.codeLength {
				cmap.codeLength = hexLen
			}
		}
		offset = start + m2[1]
	}
	return nil
}

// parseCodespace 解析 begincodespacerange...endcodespacerange 段，
// 确定编码的字节长度（如 1 字节、2 字节等）
func parseCodespace(data []byte, cmap *CMap) {
	rx := regexp.MustCompile(`begincodespacerange\s*((?:\s*<[0-9A-Fa-f]+>\s*<[0-9A-Fa-f]+>\s*)+)\s*endcodespacerange`)
	m := rx.FindSubmatch(data)
	if m == nil {
		return
	}
	pairs := hexPairRx.FindAllSubmatch(m[1], -1)
	if len(pairs) >= 2 {
		startLen := len(string(pairs[0][1]))
		endLen := len(string(pairs[1][1]))
		if startLen > cmap.codeLength {
			cmap.codeLength = startLen
		}
		if endLen > cmap.codeLength {
			cmap.codeLength = endLen
		}
	}
}

// CodeBytes 返回每个输入编码的字节数。
// 十六进制长度 ≤ 2 时为 1 字节，否则为 hexLength/2 字节。
func (c *CMap) CodeBytes() int {
	if c.codeLength <= 2 {
		return 1
	}
	return c.codeLength / 2
}

// DecodeSingle 将单个输入编码解码为 Unicode 码点。
// 先查 bfchar 单个映射，再查 bfrange 范围映射。
// 返回 0 表示未找到映射。
func (c *CMap) DecodeSingle(code int) rune {
	// 先查找单个映射
	if r, ok := c.singleMappings[code]; ok {
		return r
	}
	// 再查找范围映射
	for _, rm := range c.rangeMappings {
		if code >= rm.Start && code <= rm.End {
			return rune(rm.Base + code - rm.Start)
		}
	}
	return 0
}

// MappingCount 返回单个映射和范围映射的数量
func (c *CMap) MappingCount() (singles, ranges int) {
	return len(c.singleMappings), len(c.rangeMappings)
}

// parseHex 将十六进制字符串解析为整数
func parseHex(s string) int {
	s = trimHex(s)
	if len(s) == 0 {
		return -1
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return -1
	}
	return int(v)
}

// trimHex 移除十六进制字符串两端的尖括号和空格
func trimHex(s string) string {
	for len(s) > 0 && (s[0] == '<' || s[0] == ' ') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '>' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// splitLines 将字节数据按换行符分割为多行
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
