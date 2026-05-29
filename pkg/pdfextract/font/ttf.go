package font

import (
	"encoding/binary"
)

// ParseTTFcmap 解析 TrueType 字体的 cmap 表，构建 GID → Unicode 的反向映射。
// 用于无 ToUnicode CMap 时的字符解码回退。
//
// 优先使用以下 cmap 子表：
//   - Platform 3 (Windows), Encoding 1 (Unicode BMP) — format 4
//   - Platform 3 (Windows), Encoding 10 (Unicode full) — format 12
//   - Platform 0 (Unicode), Encoding 3/4/6 — format 4 或 12
//
// 返回 GID → Unicode 映射表。如果解析失败返回 nil。
func ParseTTFcmap(data []byte) map[int]rune {
	if len(data) < 12 {
		return nil
	}

	// 读取 offset table
	_ = binary.BigEndian.Uint32(data[0:4]) // sfVersion
	numTables := binary.BigEndian.Uint16(data[4:6])

	// 查找 'cmap' 表
	var cmapOffset uint32
	var cmapLength uint32
	for i := 0; i < int(numTables); i++ {
		off := 12 + i*16
		if off+16 > len(data) {
			break
		}
		tag := string(data[off : off+4])
		if tag == "cmap" {
			cmapOffset = binary.BigEndian.Uint32(data[off+8 : off+12])
			cmapLength = binary.BigEndian.Uint32(data[off+12 : off+16])
			break
		}
	}
	if cmapOffset == 0 || cmapOffset+4 > uint32(len(data)) {
		return nil
	}
	if cmapLength > uint32(len(data))-cmapOffset {
		cmapLength = uint32(len(data)) - cmapOffset
	}

	return parseCmapTable(data[cmapOffset : cmapOffset+cmapLength])
}

// parseCmapTable 解析 cmap 表，选择最佳子表并提取 GID → Unicode 映射。
func parseCmapTable(data []byte) map[int]rune {
	if len(data) < 4 {
		return nil
	}
	_ = binary.BigEndian.Uint16(data[0:2]) // version
	numSubtables := binary.BigEndian.Uint16(data[2:4])

	type subtableInfo struct {
		platformID uint16
		encodingID uint16
		offset     uint32
	}
	var subtables []subtableInfo
	for i := 0; i < int(numSubtables); i++ {
		off := 4 + i*8
		if off+8 > len(data) {
			break
		}
		platformID := binary.BigEndian.Uint16(data[off : off+2])
		encodingID := binary.BigEndian.Uint16(data[off+2 : off+4])
		offset := binary.BigEndian.Uint32(data[off+4 : off+8])
		subtables = append(subtables, subtableInfo{platformID, encodingID, offset})
	}

	// 优先级排序：Windows Unicode full > Windows Unicode BMP > Unicode
	// Windows platform 3 encoding 10 (format 12, full Unicode) 最优先
	var bestFormat12 *subtableInfo
	var bestFormat4 *subtableInfo
	for i := range subtables {
		st := &subtables[i]
		if st.platformID == 3 && st.encodingID == 10 {
			bestFormat12 = st
			break
		}
		if st.platformID == 0 && (st.encodingID == 4 || st.encodingID == 6) {
			if bestFormat12 == nil {
				bestFormat12 = st
			}
		}
	}
	for i := range subtables {
		st := &subtables[i]
		if st.platformID == 3 && st.encodingID == 1 {
			bestFormat4 = st
			break
		}
		if st.platformID == 0 && st.encodingID == 3 {
			if bestFormat4 == nil {
				bestFormat4 = st
			}
		}
	}

	// 优先使用 format 12（支持完整 Unicode），然后 format 4
	if bestFormat12 != nil {
		if m := parseCmapSubtable(data, bestFormat12.offset); len(m) > 0 {
			return m
		}
	}
	if bestFormat4 != nil {
		if m := parseCmapSubtable(data, bestFormat4.offset); len(m) > 0 {
			return m
		}
	}

	// 回退：尝试所有子表
	for i := range subtables {
		if m := parseCmapSubtable(data, subtables[i].offset); len(m) > 0 {
			return m
		}
	}
	return nil
}

// parseCmapSubtable 解析指定偏移处的 cmap 子表。
func parseCmapSubtable(data []byte, offset uint32) map[int]rune {
	if int(offset)+4 > len(data) {
		return nil
	}
	format := binary.BigEndian.Uint16(data[offset : offset+2])

	switch format {
	case 0:
		return parseCmapFormat0(data, offset)
	case 4:
		return parseCmapFormat4(data, offset)
	case 6:
		return parseCmapFormat6(data, offset)
	case 12:
		return parseCmapFormat12(data, offset)
	default:
		return nil
	}
}

// parseCmapFormat0 解析 format 0 子表（字节编码表，256 字节）。
func parseCmapFormat0(data []byte, offset uint32) map[int]rune {
	if int(offset)+262 > len(data) {
		return nil
	}
	// format(2) + length(2) + language(2) + glyphIndexArray(256)
	gidToUnicode := make(map[int]rune)
	for i := 0; i < 256; i++ {
		gid := int(data[int(offset)+6+i])
		if gid > 0 && gidToUnicode[gid] == 0 {
			gidToUnicode[gid] = rune(i)
		}
	}
	return gidToUnicode
}

// parseCmapFormat4 解析 format 4 子表（段映射，用于 BMP 字符）。
// 构建反向映射 GID → Unicode。
func parseCmapFormat4(data []byte, offset uint32) map[int]rune {
	base := int(offset)
	if base+14 > len(data) {
		return nil
	}
	_ = binary.BigEndian.Uint16(data[base : base+2])   // format
	_ = binary.BigEndian.Uint16(data[base+2 : base+4]) // length
	_ = binary.BigEndian.Uint16(data[base+4 : base+6]) // language
	segCountX2 := int(binary.BigEndian.Uint16(data[base+6 : base+8]))
	segCount := segCountX2 / 2
	_ = binary.BigEndian.Uint16(data[base+8 : base+10])  // searchRange
	_ = binary.BigEndian.Uint16(data[base+10 : base+12]) // entrySelector
	_ = binary.BigEndian.Uint16(data[base+12 : base+14]) // rangeShift

	endCodeOff := base + 14
	startCodeOff := endCodeOff + segCountX2 + 2 // +2 for reservedPad
	idDeltaOff := startCodeOff + segCountX2
	idRangeOffOff := idDeltaOff + segCountX2

	if idRangeOffOff+segCountX2 > len(data) {
		return nil
	}

	gidToUnicode := make(map[int]rune)

	for i := 0; i < segCount; i++ {
		endCode := int(binary.BigEndian.Uint16(data[endCodeOff+i*2 : endCodeOff+i*2+2]))
		startCode := int(binary.BigEndian.Uint16(data[startCodeOff+i*2 : startCodeOff+i*2+2]))
		idDelta := int16(binary.BigEndian.Uint16(data[idDeltaOff+i*2 : idDeltaOff+i*2+2]))
		idRangeOffset := int(binary.BigEndian.Uint16(data[idRangeOffOff+i*2 : idRangeOffOff+i*2+2]))

		// 0xFFFF 终止段
		if startCode == 0xFFFF {
			break
		}

		for c := startCode; c <= endCode; c++ {
			var gid int
			if idRangeOffset == 0 {
				gid = int(int16(c) + idDelta)
			} else {
				// 计算 glyphIndexArray 的偏移
				arrayOff := idRangeOffOff + i*2 + idRangeOffset + (c-startCode)*2
				if arrayOff+2 > len(data) {
					continue
				}
				gid = int(binary.BigEndian.Uint16(data[arrayOff : arrayOff+2]))
				if gid != 0 {
					gid = int(int16(gid) + idDelta)
				}
			}

			// 跳过 GID 0 (.notdef)
			if gid <= 0 || gid > 0xFFFF {
				continue
			}

			// 反向映射：GID → Unicode
			// 只在尚未映射时设置，优先保留已有映射（段顺序中靠前的优先）
			if gidToUnicode[gid] == 0 {
				gidToUnicode[gid] = rune(c)
			}
		}
	}

	return gidToUnicode
}

// parseCmapFormat6 解析 format 6 子表（修剪表映射）。
func parseCmapFormat6(data []byte, offset uint32) map[int]rune {
	base := int(offset)
	if base+12 > len(data) {
		return nil
	}
	_ = binary.BigEndian.Uint16(data[base : base+2])   // format
	_ = binary.BigEndian.Uint16(data[base+2 : base+4]) // length
	_ = binary.BigEndian.Uint16(data[base+4 : base+6]) // language
	firstCode := int(binary.BigEndian.Uint16(data[base+6 : base+8]))
	entryCount := int(binary.BigEndian.Uint16(data[base+8 : base+10]))

	if base+10+entryCount*2 > len(data) {
		return nil
	}

	gidToUnicode := make(map[int]rune)
	for i := 0; i < entryCount; i++ {
		gid := int(binary.BigEndian.Uint16(data[base+10+i*2 : base+10+i*2+2]))
		if gid > 0 {
			unicode := rune(firstCode + i)
			if gidToUnicode[gid] == 0 {
				gidToUnicode[gid] = unicode
			}
		}
	}
	return gidToUnicode
}

// parseCmapFormat12 解析 format 12 子表（分段覆盖，支持完整 Unicode）。
func parseCmapFormat12(data []byte, offset uint32) map[int]rune {
	base := int(offset)
	if base+16 > len(data) {
		return nil
	}
	_ = binary.BigEndian.Uint16(data[base : base+2])   // format
	_ = binary.BigEndian.Uint16(data[base+2 : base+4]) // reserved
	_ = binary.BigEndian.Uint32(data[base+4 : base+8]) // length
	_ = binary.BigEndian.Uint32(data[base+8 : base+12]) // language
	numGroups := int(binary.BigEndian.Uint32(data[base+12 : base+16]))

	if base+16+numGroups*12 > len(data) {
		return nil
	}

	gidToUnicode := make(map[int]rune)
	for i := 0; i < numGroups; i++ {
		groupOff := base + 16 + i*12
		startCharCode := binary.BigEndian.Uint32(data[groupOff : groupOff+4])
		endCharCode := binary.BigEndian.Uint32(data[groupOff+4 : groupOff+8])
		startGlyphCode := binary.BigEndian.Uint32(data[groupOff+8 : groupOff+12])

		for c := startCharCode; c <= endCharCode; c++ {
			gid := int(startGlyphCode + (c - startCharCode))
			if gid <= 0 {
				continue
			}
			if gidToUnicode[gid] == 0 {
				gidToUnicode[gid] = rune(c)
			}
		}
	}
	return gidToUnicode
}

// BuildCMapFromGIDMapping 从 GID→Unicode 映射构建 font.CMap 对象。
// 用于 CID 字体中，当 GID 等于 CID 时（如 Identity-H 编码），
// 可以直接使用此映射来解码字符。
//
// codeBytes 参数指定编码字节长度（1 用于简单字体，2 用于 CID 字体）。
func BuildCMapFromGIDMapping(gidToUnicode map[int]rune, codeBytes int) *CMap {
	if len(gidToUnicode) == 0 {
		return nil
	}
	singleMappings := make(map[int][]rune, len(gidToUnicode))
	for gid, r := range gidToUnicode {
		if r > 0 {
			singleMappings[gid] = []rune{r}
		}
	}
	if len(singleMappings) == 0 {
		return nil
	}

	// codeLength 是十六进制位数
	codeLength := codeBytes * 2
	if codeLength < 2 {
		codeLength = 2
	}

	return &CMap{
		singleMappings: singleMappings,
		codeLength:     codeLength,
	}
}

// BuildEncodingFromGIDMapping 从 GID→Unicode 映射为简单字体构建 byte→rune 编码表。
// 简单字体使用单字节编码，GID 范围限定在 0-255。
func BuildEncodingFromGIDMapping(gidToUnicode map[int]rune) map[byte]rune {
	encoding := make(map[byte]rune, 256)
	for gid, r := range gidToUnicode {
		if gid >= 0 && gid <= 255 && r > 0 {
			encoding[byte(gid)] = r
		}
	}
	if len(encoding) == 0 {
		return nil
	}
	return encoding
}

