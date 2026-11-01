package store

import (
	"os"
)

// UpdatePbStore 安全更新 agyhub_summaries_proto.pb 文件中的指定会话标题。
func UpdatePbStore(pbPath string, renameMap map[string]string) (int, error) {
	data, err := os.ReadFile(pbPath)
	if err != nil {
		return 0, err
	}

	root := parseProtoFields(data)
	if len(root) == 0 {
		return 0, nil
	}

	var newRoot []protoField
	updatedCount := 0

	for _, f := range root {
		if f.Num == 1 && f.WireType == 2 {
			entryBytes, ok := f.Value.([]byte)
			if ok {
				mapEntry := parseProtoFields(entryBytes)
				var cid string
				for _, me := range mapEntry {
					if me.Num == 1 && me.WireType == 2 {
						cidBytes, ok := me.Value.([]byte)
						if ok {
							cid = string(cidBytes)
						}
					}
				}

				if newTitle, found := renameMap[cid]; found && cid != "" {
					var newMapEntry []protoField
					for _, me := range mapEntry {
						if me.Num == 2 && me.WireType == 2 {
							valBytes, _ := me.Value.([]byte)
							newSummary := UpdateSummaryTitle(valBytes, newTitle)
							newMapEntry = append(newMapEntry, protoField{Num: 2, WireType: 2, Value: newSummary})
						} else {
							newMapEntry = append(newMapEntry, me)
						}
					}
					newRoot = append(newRoot, protoField{Num: 1, WireType: 2, Value: encodeFields(newMapEntry)})
					updatedCount++
				} else {
					newRoot = append(newRoot, f)
				}
			} else {
				newRoot = append(newRoot, f)
			}
		} else {
			newRoot = append(newRoot, f)
		}
	}

	if updatedCount > 0 {
		newBytes := encodeFields(newRoot)
		if err := os.WriteFile(pbPath, newBytes, 0644); err != nil {
			return 0, err
		}
	}

	return updatedCount, nil
}
