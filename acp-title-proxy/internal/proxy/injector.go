package proxy

import (
	"encoding/json"
	"io"
	"sync"

	"language_projects/go_projects/acp-title-proxy/internal/protocol"
)

// Injector manages thread-safe writes to the ACP client (Zed).
type Injector struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewInjector(w io.Writer) *Injector {
	return &Injector{writer: w}
}

// InjectTitle pushes a session_info_update notification with the given title.
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

// WriteRaw safely writes an intact raw message line to the output stream.
func (inj *Injector) WriteRaw(data []byte) (int, error) {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	return inj.writer.Write(data)
}
