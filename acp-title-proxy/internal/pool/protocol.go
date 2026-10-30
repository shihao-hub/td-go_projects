package pool

const (
	DefaultPort = 49281
)

type PoolRequest struct {
	Cmd   string `json:"cmd"`   // e.g. "acquire"
	Agent string `json:"agent"` // e.g. "agy"
}

type PoolResponse struct {
	Status string `json:"status"` // "ok" or "error"
	Msg    string `json:"msg,omitempty"`
}
