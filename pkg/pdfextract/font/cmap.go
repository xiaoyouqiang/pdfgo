package font

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
)

// CMap 实现 PDF ToUnicode CMap 的解析和解码。
type CMap struct {
	singleMappings map[int][]rune // 支持 1 对多映射（连字符如 "fi" → []rune{'f','i'}）
	rangeMappings  []RangeMapping
	codeLength     int
}

type RangeMapping struct {
	Start int
	End   int
	Base  int
}

var (
	beginbfcharRx  = regexp.MustCompile(`beginbfchar\s*`)
	endbfcharRx    = regexp.MustCompile(`endbfchar\s*`)
	beginbfrangeRx = regexp.MustCompile(`beginbfrange\s*`)
	endbfrangeRx   = regexp.MustCompile(`endbfrange\s*`)
	hexPairRx      = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

func ParseCMap(data []byte) (*CMap, error) {
	cmap := &CMap{
		singleMappings: make(map[int][]rune),
		codeLength:     1,
	}
	data = stripComments(data)
	if err := parsebfchar(data, cmap); err != nil {
		return nil, err
	}
	if err := parsebfrange(data, cmap); err != nil {
		return nil, err
	}
	parseCodespace(data, cmap)
	return cmap, nil
}

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
		pairs := hexPairRx.FindAllSubmatch(segment, -1)
		for i := 0; i+1 < len(pairs); i += 2 {
			src := parseHex(string(pairs[i][1]))
			if src < 0 {
				continue
			}
			runes := parseHexToRunes(string(pairs[i+1][1]))
			if len(runes) > 0 {
				cmap.singleMappings[src] = runes
			}
			hexLen := len(string(pairs[i][1]))
			if hexLen > cmap.codeLength {
				cmap.codeLength = hexLen
			}
		}
		offset = start + m2[1]
	}
	return nil
}


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
			startCode := parseHex(string(pairs[0][1]))
			endCode := parseHex(string(pairs[1][1]))
			if startCode < 0 || endCode < 0 || startCode > endCode || endCode-startCode > 65536 {
				continue
			}
			// PDF 规范 bfrange 有两种格式：
			// 1) 范围格式：<start> <end> <base> — 连续映射
			// 2) 数组格式：<start> <end> [<u1> <u2> ...] — 逐个映射
			if len(pairs) > 3 && bytes.Contains(line, []byte{'['}) {
				for i := 0; i+2 < len(pairs) && startCode+i <= endCode; i++ {
					runes := parseHexToRunes(string(pairs[i+2][1]))
					if len(runes) > 0 {
						cmap.singleMappings[startCode+i] = runes
						hexLen := len(string(pairs[i+2][1]))
						if hexLen > cmap.codeLength {
							cmap.codeLength = hexLen
						}
					}
				}
			} else {
				base := parseHex(string(pairs[2][1]))
				if base < 0 {
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
		}
		offset = start + m2[1]
	}
	return nil
}
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

func (c *CMap) CodeBytes() int {
	if c.codeLength <= 2 {
		return 1
	}
	return (c.codeLength + 1) / 2
}

func (c *CMap) DecodeSingle(code int) rune {
	runes := c.Decode(code)
	if len(runes) > 0 {
		return runes[0]
	}
	return 0
}

// Decode 将编码值解码为 Unicode 字符切片，支持一对多映射（如连字符 "fi"）。
func (c *CMap) Decode(code int) []rune {
	if runes, ok := c.singleMappings[code]; ok {
		return runes
	}
	for _, rm := range c.rangeMappings {
		if code >= rm.Start && code <= rm.End {
			decoded := rm.Base + code - rm.Start
			if decoded < 0 || decoded > 0x10FFFF {
				return nil
			}
			return []rune{rune(decoded)}
		}
	}
	return nil
}

func (c *CMap) MappingCount() (singles, ranges int) {
	return len(c.singleMappings), len(c.rangeMappings)
}

func (c *CMap) AllSingles() map[int][]rune {
	return c.singleMappings
}

func (c *CMap) AllRanges() []RangeMapping {
	return c.rangeMappings
}

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

// parseHexToRunes 将 CMap 中的十六进制值解析为 rune 切片。
// ToUnicode CMap 中一个编码可映射到多个 Unicode 码点（如连字符 "fi" = 00660069），
// 每 4 个十六进制位代表一个 BMP 字符。
func parseHexToRunes(hex string) []rune {
	hex = trimHex(hex)
	if len(hex) == 0 {
		return nil
	}
	var runes []rune
	for i := 0; i+4 <= len(hex); i += 4 {
		v, err := strconv.ParseUint(hex[i:i+4], 16, 16)
		if err != nil {
			continue
		}
		runes = append(runes, rune(v))
	}
	return runes
}

func trimHex(s string) string {
	for len(s) > 0 && (s[0] == '<' || s[0] == ' ') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '>' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

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
