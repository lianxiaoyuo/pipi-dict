package freeonline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hoverdict/internal/textsegments"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client queries the documented, unauthenticated MyMemory translation endpoint.
// The anonymous public tier has usage limits; no API secret is bundled.
// A word is sent only after offline lookup misses, or after an explicit selection.
type Client struct {
	HTTP        *http.Client
	Endpoint    string
	BeforeChunk func(string) error
}

type response struct {
	ResponseData struct {
		TranslatedText string `json:"translatedText"`
	} `json:"responseData"`
	ResponseStatus  int    `json:"responseStatus"`
	ResponseDetails string `json:"responseDetails"`
	QuotaFinished   bool   `json:"quotaFinished"`
}

func (c Client) Translate(ctx context.Context, word string) (string, error) {
	if word == "" || len([]byte(word)) > 500 || strings.ContainsAny(word, "\r\n\t") {
		return "", errors.New("无效英文单词")
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = "https://api.mymemory.translated.net/get"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("q", word)
	q.Set("langpair", "en|zh-CN")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HoverDict/3.7 (+offline-first dictionary)")
	client := c.HTTP
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("免费翻译服务连接失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("免费翻译服务 HTTP %d", resp.StatusCode)
	}
	var data response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32*1024)).Decode(&data); err != nil {
		return "", fmt.Errorf("免费翻译返回无效：%w", err)
	}
	if data.QuotaFinished {
		return "", errors.New("免费翻译今日额度已用完")
	}
	if data.ResponseStatus != 0 && data.ResponseStatus != 200 {
		return "", fmt.Errorf("免费翻译服务错误：%s (%d)", data.ResponseDetails, data.ResponseStatus)
	}
	result := strings.TrimSpace(html.UnescapeString(data.ResponseData.TranslatedText))
	if result == "" || strings.EqualFold(result, word) {
		return "", errors.New("免费翻译未返回有效中文释义")
	}
	return result, nil
}

// A shared HTTP client pools TLS connections across word and paragraph requests.
var defaultClient = &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{
	MaxIdleConns: 12, MaxIdleConnsPerHost: 5, IdleConnTimeout: 90 * time.Second,
	TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second,
}}

// TranslateText deliberately processes selected paragraphs in API-sized chunks.
// The entire selection is not sent until the user actually highlights it.
func (c Client) TranslateText(ctx context.Context, text string) (string, error) {
	chunks, err := textsegments.Split(text)
	if err != nil {
		return "", err
	}
	results := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if c.BeforeChunk != nil {
			if err := c.BeforeChunk(chunk); err != nil {
				return "", err
			}
		}
		res, e := c.Translate(ctx, chunk)
		if e != nil {
			return "", fmt.Errorf("段落翻译在第 %d/%d 段失败：%w", len(results)+1, len(chunks), e)
		}
		results = append(results, res)
	}
	return strings.Join(results, "\n"), nil
}
