package research

import (
	"deepseeksearch/internal/deepseek"
)

const MaxQueries = 6
const DefaultCount = 10

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string     { return e.Message }
func invalid(message string) error { return &Error{Code: "invalid_argument", Message: message} }

type Query struct {
	ID       string `json:"id"`
	Query    string `json:"query"`
	Reason   string `json:"reason"`
	Selected bool   `json:"selected"`
}
type Draft struct {
	Queries      []Query `json:"queries"`
	Count        int     `json:"count"`
	NeedContent  bool    `json:"needContent"`
	Localization string  `json:"localization"`
	Revision     int64   `json:"revision,omitempty"`
}
type Feedback struct {
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	ResultID  string `json:"resultId,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}
type Execution struct {
	Query
	Status    string                      `json:"status"`
	Error     string                      `json:"error,omitempty"`
	FromCache bool                        `json:"fromCache"`
	Items     []deepseek.SearchResultItem `json:"items"`
}
type Round struct {
	Number       int         `json:"number"`
	Status       string      `json:"status"`
	Count        int         `json:"count"`
	NeedContent  bool        `json:"needContent"`
	Localization string      `json:"localization"`
	StartedAt    string      `json:"startedAt"`
	Executions   []Execution `json:"executions"`
}
type Source struct {
	Round   int     `json:"round"`
	QueryID string  `json:"queryId"`
	Query   string  `json:"query"`
	Rank    int     `json:"rank"`
	Score   float64 `json:"score"`
}
type Result struct {
	deepseek.SearchResultItem
	ID          string   `json:"id"`
	FirstRound  int      `json:"firstRound"`
	FusionScore float64  `json:"fusionScore"`
	Sources     []Source `json:"sources"`
}
type Session struct {
	ID        string     `json:"id"`
	Question  string     `json:"question"`
	Revision  int64      `json:"revision"`
	UpdatedAt string     `json:"updatedAt"`
	Busy      bool       `json:"busy"`
	Draft     Draft      `json:"draft"`
	Rounds    []Round    `json:"rounds"`
	Feedback  []Feedback `json:"feedback"`
	Results   []Result   `json:"results"`
}
type RunOptions struct {
	RetryFailed bool `json:"retryFailed"`
	Refresh     bool `json:"refresh"`
}
