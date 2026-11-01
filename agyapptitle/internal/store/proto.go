package store

import (
	"bytes"
)

// protoField 表示解析出的单个 Protobuf 字段。
type protoField struct {
	Num      int
	WireType int
	Value    interface{} // uint64 (type 0), []byte (type 1, 2, 5)
}

// parseProtoFields 解析 Protobuf 原始字节流为字段列表。
func parseProtoFields(data []byte) []protoField {
	var fields []protoField
	idx := 0
	n := len(data)

	for idx < n {
		var key uint64
		var shift uint
		validKey := false
		for idx < n {
			b := data[idx]
			idx++
			key |= uint64(b&0x7F) << shift
			if (b & 0x80) == 0 {
				validKey = true
				break
			}
			shift += 7
		}
		if !validKey || key == 0 {
			break
		}

		fieldNum := int(key >> 3)
		wireType := int(key & 0x7)

		switch wireType {
		case 0: // Varint
			var val uint64
			var s uint
			validVal := false
			for idx < n {
				b := data[idx]
				idx++
				val |= uint64(b&0x7F) << s
				if (b & 0x80) == 0 {
					validVal = true
					break
				}
				s += 7
			}
			if validVal {
				fields = append(fields, protoField{Num: fieldNum, WireType: wireType, Value: val})
			}
		case 1: // 64-bit
			if idx+8 <= n {
				val := make([]byte, 8)
				copy(val, data[idx:idx+8])
				idx += 8
				fields = append(fields, protoField{Num: fieldNum, WireType: wireType, Value: val})
			}
		case 2: // Length-delimited
			var length uint64
			var s uint
			validLen := false
			for idx < n {
				b := data[idx]
				idx++
				length |= uint64(b&0x7F) << s
				if (b & 0x80) == 0 {
					validLen = true
					break
				}
				s += 7
			}
			if validLen && idx+int(length) <= n {
				val := make([]byte, length)
				copy(val, data[idx:idx+int(length)])
				idx += int(length)
				fields = append(fields, protoField{Num: fieldNum, WireType: wireType, Value: val})
			}
		case 5: // 32-bit
			if idx+4 <= n {
				val := make([]byte, 4)
				copy(val, data[idx:idx+4])
				idx += 4
				fields = append(fields, protoField{Num: fieldNum, WireType: wireType, Value: val})
			}
		default:
			return fields
		}
	}
	return fields
}

// encodeVarint 编码无符号整型为 Varint 字节。
func encodeVarint(val uint64) []byte {
	var buf bytes.Buffer
	for val >= 0x80 {
		buf.WriteByte(byte(val&0x7F) | 0x80)
		val >>= 7
	}
	buf.WriteByte(byte(val & 0x7F))
	return buf.Bytes()
}

// encodeFields 将字段列表重构回 Protobuf 二进制流。
func encodeFields(fields []protoField) []byte {
	var buf bytes.Buffer
	for _, f := range fields {
		key := uint64((f.Num << 3) | f.WireType)
		buf.Write(encodeVarint(key))
		switch f.WireType {
		case 0:
			if v, ok := f.Value.(uint64); ok {
				buf.Write(encodeVarint(v))
			}
		case 1:
			if v, ok := f.Value.([]byte); ok {
				buf.Write(v)
			}
		case 2:
			if v, ok := f.Value.([]byte); ok {
				buf.Write(encodeVarint(uint64(len(v))))
				buf.Write(v)
			}
		case 5:
			if v, ok := f.Value.([]byte); ok {
				buf.Write(v)
			}
		}
	}
	return buf.Bytes()
}

// UpdateSummaryTitle 更新 CascadeTrajectorySummary 二进制流中的 Field 1 (title) 与 Field 15 (annotations.title)。
func UpdateSummaryTitle(rawBytes []byte, newTitle string) []byte {
	if len(rawBytes) == 0 {
		return rawBytes
	}
	fields := parseProtoFields(rawBytes)
	if len(fields) == 0 {
		return rawBytes
	}

	titleBytes := []byte(newTitle)
	var newFields []protoField
	hasF1 := false

	for _, f := range fields {
		if f.Num == 1 && f.WireType == 2 {
			newFields = append(newFields, protoField{Num: 1, WireType: 2, Value: titleBytes})
			hasF1 = true
		} else if f.Num == 15 && f.WireType == 2 {
			// Field 15: annotations sub-message
			subBytes, ok := f.Value.([]byte)
			if ok {
				subFields := parseProtoFields(subBytes)
				var newSub []protoField
				hasSub1 := false
				for _, sf := range subFields {
					if sf.Num == 1 && sf.WireType == 2 {
						newSub = append(newSub, protoField{Num: 1, WireType: 2, Value: titleBytes})
						hasSub1 = true
					} else {
						newSub = append(newSub, sf)
					}
				}
				if !hasSub1 {
					newSub = append([]protoField{{Num: 1, WireType: 2, Value: titleBytes}}, newSub...)
				}
				newFields = append(newFields, protoField{Num: 15, WireType: 2, Value: encodeFields(newSub)})
			} else {
				newFields = append(newFields, f)
			}
		} else {
			newFields = append(newFields, f)
		}
	}

	if !hasF1 {
		newFields = append([]protoField{{Num: 1, WireType: 2, Value: titleBytes}}, newFields...)
	}

	return encodeFields(newFields)
}
