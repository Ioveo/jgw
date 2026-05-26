// Package notify 多渠道通知（控制台、Webhook、Telegram）
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Config 通知配置
type Config struct {
	WebhookURL       string // 企微/钉钉 Webhook
	TelegramBotToken string
	TelegramChatID   string
}

// Notifier 通知发送器
type Notifier struct {
	cfg    Config
	client *http.Client
}

// New 创建通知发送器
func New(cfg Config) *Notifier {
	return &Notifier{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Send 发送通知到所有已配置渠道
func (n *Notifier) Send(title, body string) {
	// 总是打印到控制台
	log.Printf("[NOTIFY] %s — %s", title, body)

	if n.cfg.WebhookURL != "" {
		if err := n.sendWebhook(title, body); err != nil {
			log.Printf("[WARN] Webhook 发送失败: %v", err)
		}
	}

	if n.cfg.TelegramBotToken != "" && n.cfg.TelegramChatID != "" {
		if err := n.sendTelegram(title, body); err != nil {
			log.Printf("[WARN] Telegram 发送失败: %v", err)
		}
	}
}

// sendWebhook 发送到企业微信/钉钉 Webhook
// 自动检测平台（钉钉包含 oapi.dingtalk，企微包含 qyapi）
func (n *Notifier) sendWebhook(title, body string) error {
	url := n.cfg.WebhookURL
	var payload interface{}
	var contentType = "application/json"

	fullText := fmt.Sprintf("**%s**\n\n%s", title, body)

	if strings.Contains(url, "qyapi") {
		// 企业微信 Markdown 格式
		payload = map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": fullText,
			},
		}
	} else if strings.Contains(url, "dingtalk") {
		// 钉钉 Markdown 格式
		payload = map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": title,
				"text":  fullText,
			},
		}
	} else {
		// 通用 JSON Payload
		payload = map[string]string{
			"title":   title,
			"content": fullText,
		}
	}

	data, _ := json.Marshal(payload)
	resp, err := n.client.Post(url, contentType, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Webhook 返回错误状态码: %d", resp.StatusCode)
	}
	return nil
}

// sendTelegram 通过 Telegram Bot API 发送消息
func (n *Notifier) sendTelegram(title, body string) error {
	text := fmt.Sprintf("*%s*\n\n%s", escapeMarkdown(title), escapeMarkdown(body))
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.cfg.TelegramBotToken)

	payload := map[string]interface{}{
		"chat_id":    n.cfg.TelegramChatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}

	data, _ := json.Marshal(payload)
	resp, err := n.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Telegram API 返回错误状态码: %d", resp.StatusCode)
	}
	return nil
}

// escapeMarkdown 转义 Telegram MarkdownV2 特殊字符
func escapeMarkdown(s string) string {
	special := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	for _, c := range special {
		s = strings.ReplaceAll(s, c, "\\"+c)
	}
	return s
}
