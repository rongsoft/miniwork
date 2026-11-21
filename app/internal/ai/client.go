package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SceneScore 是打分场景。客户端只传 scene，不传 model：
// 以后要在服务端把打分换成便宜模型、写稿换成强模型，改一行配置就行，不用发版。
const SceneScore = "score"

// SceneCraft 是成稿场景。单独一个 scene，是因为它和打分对模型的要求相反：
// 打分要克制（低温度、短输出），写稿要发散（高温度、长输出）。
const SceneCraft = "craft"

// 模型响应慢是常态，但桌面端不能无限等：45 秒还没回来，就走规则分。
const requestTimeout = 45 * time.Second

// 写一篇稿子比给 50 条打分慢得多：输出上千字时 45 秒不够，
// 而用户点了「生成」之后盯着转圈等两分钟，比等不到要好。
const craftTimeout = 120 * time.Second

// temperature 压得低一些：打分要稳定，不要创意。
const temperature = 0.3

// 写稿反过来要放开。0.85 是「有明显变化但仍守主题」的位置：
// 再高会开始编经历，再低八篇稿子会长得一模一样。
const craftTemperature = 0.85

// Relay 走我们自己的服务端。Key 在服务端，客户端只拿 license token。
type Relay struct {
	base     string
	token    string
	http     *http.Client
	creative *http.Client
}

func NewRelay(base, token string) *Relay {
	return &Relay{
		base:     strings.TrimRight(base, "/"),
		token:    token,
		http:     &http.Client{Timeout: requestTimeout},
		creative: &http.Client{Timeout: craftTimeout},
	}
}

func (r *Relay) Name() string { return "relay" }

func (r *Relay) Complete(ctx context.Context, system, user string) (string, error) {
	if r.base == "" {
		return "", ErrNotConfigured
	}
	body, err := json.Marshal(map[string]any{
		"scene":       SceneScore,
		"temperature": temperature,
		"messages":    messages(system, user),
	})
	if err != nil {
		return "", err
	}
	return r.post(ctx, r.base+"/ai/relay", r.token, body)
}

// CompleteCreative 走同一套接口，只换 scene 与温度。
// 服务端据此路由到更适合长文的模型；没配也照样能用，只是模型不变。
func (r *Relay) CompleteCreative(ctx context.Context, system, user string) (string, error) {
	if r.base == "" {
		return "", ErrNotConfigured
	}
	body, err := json.Marshal(map[string]any{
		"scene":       SceneCraft,
		"temperature": craftTemperature,
		"messages":    messages(system, user),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.base+"/ai/relay", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Miniwork/0.1")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	return do(r.creative, req)
}

func (r *Relay) post(ctx context.Context, url, token string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Miniwork/0.1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return do(r.http, req)
}

// Byok 是用户自带 Key 直连 OpenAI 兼容接口。
// 保留这条路，是因为总有人不想把内容过第三方服务，也总有人想自己选模型。
type Byok struct {
	base     string
	key      string
	model    string
	http     *http.Client
	creative *http.Client
}

func NewByok(base, key, model string) *Byok {
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &Byok{
		base:     strings.TrimRight(base, "/"),
		key:      key,
		model:    model,
		http:     &http.Client{Timeout: requestTimeout},
		creative: &http.Client{Timeout: craftTimeout},
	}
}

func (b *Byok) Name() string { return "byok" }

// ModelName 是上游模型名，如 deepseek-chat。界面用它，不用 "byok"。
func (b *Byok) ModelName() string { return b.model }

func (b *Byok) Complete(ctx context.Context, system, user string) (string, error) {
	return b.send(ctx, b.http, system, user, temperature, 0)
}

// CompleteCreative 写稿用。max_tokens 显式给足：
// 部分 OpenAI 兼容服务默认只给 1024，一千字的长文会被腰斩在半句话上，
// 而那种截断不会报错，用户只会看到一篇写到一半的稿子。
func (b *Byok) CompleteCreative(ctx context.Context, system, user string) (string, error) {
	return b.send(ctx, b.creative, system, user, craftTemperature, 4096)
}

func (b *Byok) send(ctx context.Context, c *http.Client, system, user string, temp float64, maxTokens int) (string, error) {
	if b.base == "" || b.key == "" {
		return "", ErrNotConfigured
	}
	payload := map[string]any{
		"model":       b.model,
		"temperature": temp,
		"messages":    messages(system, user),
	}
	if maxTokens > 0 {
		payload["max_tokens"] = maxTokens
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.key)
	return do(c, req)
}

func messages(system, user string) []map[string]string {
	return []map[string]string{
		{"role": "system", "content": system},
		{"role": "user", "content": user},
	}
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// do 发请求并把 OpenAI 兼容响应里的正文取出来。
// 状态码映射必须精确：402/429 是「今天别试了」，401 是「配错了」，
// 两种情况用户要做的事完全不同，混成一个错误等于让用户去猜。
func do(c *http.Client, req *http.Request) (string, error) {
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("模型服务连不上: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取模型响应失败: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusPaymentRequired, http.StatusTooManyRequests:
		return "", ErrQuotaExceeded
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrUnauthorized
	default:
		return "", fmt.Errorf("模型服务返回 %d: %s", resp.StatusCode, snippet(raw))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("模型响应不是合法 JSON: %w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("模型报错: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("模型没有返回内容: %s", snippet(raw))
	}
	return cr.Choices[0].Message.Content, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	const max = 160
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// New 按配置建客户端。mode 为 off 或配置不全时返回 nil，调用方按「没模型」处理。
func New(mode, relayURL, relayToken, baseURL, apiKey, model string) Client {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "relay":
		if strings.TrimSpace(relayURL) == "" {
			return nil
		}
		return NewRelay(relayURL, relayToken)
	case "byok":
		if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" {
			return nil
		}
		return NewByok(baseURL, apiKey, model)
	}
	return nil
}
