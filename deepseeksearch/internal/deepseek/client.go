package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.deepseek.com"
	SearchPath     = "/v1/tools/search"
	FetchPath      = "/v1/tools/fetch"
	ModelsPath     = "/models"
)

// Client DeepSeek API 客户端
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey, baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// SearchRequest 搜索请求参数
type SearchRequest struct {
	Query        string `json:"query"`
	Count        int    `json:"count,omitempty"`
	OriQuestion  string `json:"ori_question,omitempty"`
	NeedContent  bool   `json:"need_content,omitempty"`
	Localization string `json:"localization,omitempty"`
}

// SearchResultItem 单个搜索条目
type SearchResultItem struct {
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	Snippet     string      `json:"snippet"`
	Summary     string      `json:"summary"`
	Content     string      `json:"content,omitempty"`
	RerankScore float64     `json:"rerank_score"`
	Authority   interface{} `json:"authority,omitempty"`
}

// SearchResponse 搜索响应
type SearchResponse struct {
	Results   []SearchResultItem `json:"results"`
	FromCache bool               `json:"from_cache,omitempty"`
	CachedAt  *time.Time         `json:"cached_at,omitempty"`
}

// FetchRequest 抓取请求参数
type FetchRequest struct {
	URL string `json:"url"`
}

// FetchResponse 抓取响应
type FetchResponse struct {
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	FromCache bool       `json:"from_cache,omitempty"`
	CachedAt  *time.Time `json:"cached_at,omitempty"`
}

type APIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
	Message string `json:"msg"`
}

func (c *Client) doRequest(ctx context.Context, method, path string, reqBody interface{}, respDest interface{}) error {
	if strings.TrimSpace(c.apiKey) == "" {
		return errors.New("尚未配置 DeepSeek API Key，请先在设置中填写")
	}

	var reqBytes []byte
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("序列化请求参数失败: %w", err)
		}
		reqBytes = data
	}

	fullURL := c.baseURL + path
	maxRetries := 2

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var bodyReader io.Reader
		if reqBytes != nil {
			bodyReader = bytes.NewReader(reqBytes)
		}

		httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return fmt.Errorf("构建请求失败: %w", err)
		}

		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if attempt < maxRetries && ctx.Err() == nil {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			}
			return fmt.Errorf("请求 DeepSeek 服务端网络异常: %w", err)
		}

		respBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("读取响应失败: %w", err)
		}

		// 遇到 429 (限流) 或 502/503 (服务端短暂不可用) 时进行退避重试
		if (resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable) && attempt < maxRetries && ctx.Err() == nil {
			time.Sleep(time.Duration(attempt+1) * 1000 * time.Millisecond)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var errResp APIErrorResponse
			if jsonErr := json.Unmarshal(respBytes, &errResp); jsonErr == nil {
				msg := errResp.Error.Message
				if msg == "" {
					msg = errResp.Message
				}
				if msg != "" {
					return fmt.Errorf("DeepSeek API 错误 (HTTP %d): %s", resp.StatusCode, msg)
				}
			}
			return fmt.Errorf("DeepSeek API 请求失败 (HTTP %d): %s", resp.StatusCode, string(respBytes))
		}

		if respDest != nil {
			if err := json.Unmarshal(respBytes, respDest); err != nil {
				return fmt.Errorf("解析响应 JSON 失败: %w", err)
			}
		}
		return nil
	}

	return errors.New("网络请求重试耗尽，未能获取成功响应")
}

// Search 执行网页搜索
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("搜索关键词不能为空")
	}
	if req.Count <= 0 {
		req.Count = 10
	}
	if req.Count > 50 {
		req.Count = 50
	}

	var resp SearchResponse
	if err := c.doRequest(ctx, http.MethodPost, SearchPath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Fetch 抓取网页并提取为 Markdown
func (c *Client) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if strings.TrimSpace(req.URL) == "" {
		return nil, errors.New("目标网页 URL 不能为空")
	}

	var resp FetchResponse
	if err := c.doRequest(ctx, http.MethodPost, FetchPath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TestAuth 测试 API Key 是否有效
// 优先使用完全免费的标准心跳端点 GET /models 探测；
// 避免打 /v1/tools/search 导致连通性测试扣取调用费用。
func (c *Client) TestAuth(ctx context.Context) error {
	key := strings.TrimSpace(c.apiKey)
	if key == "" {
		return errors.New("API Key 不能为空")
	}

	// 1. 优先调用免费的 /models 端点验证 Key 鉴权有效性
	if err := c.doRequest(ctx, http.MethodGet, ModelsPath, nil, nil); err != nil {
		// 若返回 404，说明可能是只代理了特定 tool 端点的反向代理，此时才兜底探测最小搜索
		if strings.Contains(err.Error(), "404") {
			req := SearchRequest{
				Query: "test",
				Count: 1,
			}
			return c.doRequest(ctx, http.MethodPost, SearchPath, req, nil)
		}
		return err
	}
	return nil
}
