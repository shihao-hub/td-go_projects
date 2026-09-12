package client

import "encoding/json"

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

// marshalParams 把参数序列化为 RawMessage；nil 返回 nil（协议里省略 params）。
func marshalParams(params any) ([]byte, error) {
	if params == nil {
		return nil, nil
	}
	switch p := params.(type) {
	case []byte:
		if len(p) == 0 {
			return nil, nil
		}
		return p, nil
	case json.RawMessage:
		if len(p) == 0 {
			return nil, nil
		}
		return p, nil
	default:
		return json.Marshal(params)
	}
}
