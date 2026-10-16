package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// send 组装并发送请求；非 200 与传输层失败都在这里转成 [*Error]。
func (p *OpenAI) send(
	ctx context.Context,
	messages []Message,
	settings settings,
) (*http.Response, error) {
	request, err := p.newRequest(ctx, messages, settings)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, &Error{Provider: p.name, Message: transportMessage(p.timeout, err), Kind: KindNetwork}
	}
	if response.StatusCode == http.StatusOK {
		return response, nil
	}
	defer response.Body.Close()
	return nil, p.statusError(response)
}

// newRequest 构造请求：端点、鉴权头、UA 与请求体都在这里。
func (p *OpenAI) newRequest(
	ctx context.Context,
	messages []Message,
	settings settings,
) (*http.Request, error) {
	body, err := json.Marshal(buildStreamBody(p.style, p.model, messages, settings))
	if err != nil {
		return nil, &Error{Provider: p.name, Message: "请求体编码失败：" + err.Error(), Kind: KindConfig}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Provider: p.name, Message: "请求构造失败：" + err.Error(), Kind: KindConfig}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("User-Agent", p.userAgent)
	return request, nil
}

// statusError 把非 200 响应转成失败，并带上服务端的 `Retry-After`。
func (p *OpenAI) statusError(response *http.Response) *Error {
	// 读失败就用已读到的部分：错误文案是诊断用的，不值得因此再抛一个错。
	body, _ := io.ReadAll(response.Body)
	return &Error{
		Provider:   p.name,
		Message:    errorMessage(response.StatusCode, string(body)),
		StatusCode: response.StatusCode,
		RetryAfter: ParseRetryAfter(response.Header.Get("Retry-After")),
	}
}

// errorMessage 提取错误文案：优先服务端结构化文案，退回原始 body，空则 `HTTP {code}`。
//
// 整段 HTML / JSON 直接抛给上层，会在屏上糊几百字符的噪声。
func errorMessage(statusCode int, body string) string {
	trimmed := strings.TrimSpace(body)
	message, ok := structuredMessage(trimmed)
	if !ok {
		return defaultText(trimmed, fmt.Sprintf("HTTP %d", statusCode))
	}
	return message
}

// structuredMessage 取服务端结构化错误文案：先看 `error.message`，再看顶层 `message`。
func structuredMessage(body string) (string, bool) {
	frame, ok := decodeFrame(body)
	if !ok {
		return "", false
	}
	if detail, ok := objectOf(frame["error"]); ok {
		if message, ok := nonEmptyString(detail["message"]); ok {
			return message, true
		}
	}
	return nonEmptyString(frame["message"])
}

// transportMessage 生成传输层失败文案：超时单独说，其余一律归类为网络错误。
//
// 超时就认 net.Error.Timeout()：context.DeadlineExceeded 与 os.ErrDeadlineExceeded 都实现它，
// 单独再比对 context 会多出一段永远够不到的分支。
func transportMessage(timeout time.Duration, err error) string {
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return "请求超时（" + timeout.String() + "）"
	}
	return "网络错误：" + err.Error()
}

// buildClient 决定实际使用的客户端：注入的优先；否则自建一个只在「等响应头」阶段计时的
// 客户端——http.Client.Timeout 会把流式 body 一起掐断，长回答会被误杀。
func buildClient(injected *http.Client, timeout time.Duration) *http.Client {
	if injected != nil {
		return injected
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if shared, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = shared.Clone()
	}
	transport.ResponseHeaderTimeout = timeout
	return &http.Client{Transport: transport}
}
