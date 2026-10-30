package pool

const (
	DefaultPort = 49281
)

type PoolRequest struct {
	Cmd        string `json:"cmd"`                    // 取值如 "acquire"、"ping"、"status"、"stop"
	Agent      string `json:"agent,omitempty"`        // 取值如 "agy"
	ExeModTime int64  `json:"exe_mod_time,omitempty"` // 客户端可执行文件的修改时间（UnixNano）
}

type PoolResponse struct {
	Status  string `json:"status"` // 取值 "ok"、"error" 或 "outdated"
	Msg     string `json:"msg,omitempty"`
	Version int64  `json:"version,omitempty"` // 守护进程可执行文件的修改时间（UnixNano）
}
