package proxy

import (
	"encoding/json"
	"io"
	"sync"

	"language_projects/go_projects/acp-title-proxy/internal/protocol"
)

// Injector 负责向 ACP 客户端（Zed）线程安全地写入数据。
type Injector struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewInjector(w io.Writer) *Injector {
	return &Injector{writer: w}
}

// InjectTitle 推送携带指定标题的 session_info_update 通知。
func (inj *Injector) InjectTitle(sessionID, title string) error {
	notif := protocol.SessionUpdateNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: protocol.SessionUpdateParams{
			SessionID: sessionID,
			Update: protocol.SessionInfoUpdate{
				SessionUpdate: "session_info_update",
				Title:         title,
			},
		},
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	inj.mu.Lock()
	defer inj.mu.Unlock()

	data = append(data, '\n')
	_, err = inj.writer.Write(data)
	return err
}

// InjectAvailableCommands 推送携带指定命令的 available_commands_update 通知。
func (inj *Injector) InjectAvailableCommands(sessionID string, commands []protocol.CommandItem) error {
	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]interface{}{
			"sessionId": sessionID,
			"update": map[string]interface{}{
				"sessionUpdate":     "available_commands_update",
				"availableCommands": commands,
			},
		},
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	inj.mu.Lock()
	defer inj.mu.Unlock()

	data = append(data, '\n')
	_, err = inj.writer.Write(data)
	return err
}

// WriteRaw 将完整的一行原始消息安全写入输出流。
func (inj *Injector) WriteRaw(data []byte) (int, error) {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	return inj.writer.Write(data)
}
