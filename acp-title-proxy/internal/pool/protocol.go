package pool

const (
	DefaultPort = 49281
)

type PoolRequest struct {
	Cmd        string `json:"cmd"`                   // e.g. "acquire", "ping", "status", "stop"
	Agent      string `json:"agent,omitempty"`       // e.g. "agy"
	ExeModTime int64  `json:"exe_mod_time,omitempty"` // Client executable modification time (UnixNano)
}

type PoolResponse struct {
	Status  string `json:"status"`            // "ok", "error", or "outdated"
	Msg     string `json:"msg,omitempty"`
	Version int64  `json:"version,omitempty"` // Daemon executable modification time (UnixNano)
}
