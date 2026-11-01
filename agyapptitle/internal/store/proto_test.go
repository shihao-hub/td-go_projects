package store

import (
	"bytes"
	"testing"
)

func TestProtobufWireCodec(t *testing.T) {
	// 构造一个模拟的 CascadeTrajectorySummary 字节流
	// Field 1 (string): "Old English Title"
	// Field 2 (varint): 12345
	var originalFields []protoField
	originalFields = append(originalFields, protoField{Num: 1, WireType: 2, Value: []byte("Old English Title")})
	originalFields = append(originalFields, protoField{Num: 2, WireType: 0, Value: uint64(12345)})

	encoded := encodeFields(originalFields)
	parsed := parseProtoFields(encoded)

	if len(parsed) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(parsed))
	}

	if string(parsed[0].Value.([]byte)) != "Old English Title" {
		t.Errorf("expected 'Old English Title', got %q", string(parsed[0].Value.([]byte)))
	}

	// 测试 UpdateSummaryTitle
	updated := UpdateSummaryTitle(encoded, "新中文标题")
	parsedUpdated := parseProtoFields(updated)

	var foundNewTitle bool
	for _, f := range parsedUpdated {
		if f.Num == 1 && f.WireType == 2 {
			if string(f.Value.([]byte)) == "新中文标题" {
				foundNewTitle = true
			}
		}
	}

	if !foundNewTitle {
		t.Errorf("failed to find updated Chinese title in raw summary")
	}
}

func TestVarintCodec(t *testing.T) {
	cases := []uint64{0, 1, 127, 128, 300, 16384, 999999}
	for _, c := range cases {
		enc := encodeVarint(c)
		fields := parseProtoFields(bytes.Join([][]byte{{byte(1<<3 | 0)}, enc}, nil))
		if len(fields) != 1 || fields[0].Value.(uint64) != c {
			t.Errorf("varint roundtrip failed for %d", c)
		}
	}
}
