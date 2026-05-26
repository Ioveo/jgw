// Package oci OCI HTTP 客户端（签名 + 请求发送）
package oci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"oci-grabber/internal/auth"
)

// ClientConfig OCI 客户端配置
type ClientConfig struct {
	TenancyOCID    string
	UserOCID       string
	Fingerprint    string
	PrivateKeyPath string
	Region         string
}

// Client OCI API 客户端
type Client struct {
	cfg     ClientConfig
	signer  *auth.Signer
	httpCli *http.Client
	baseURL string
}

// NewClient 创建 OCI 客户端
func NewClient(cfg ClientConfig) *Client {
	signer, err := auth.NewSigner(cfg.TenancyOCID, cfg.UserOCID, cfg.Fingerprint, cfg.PrivateKeyPath)
	if err != nil {
		panic(fmt.Sprintf("[FATAL] 初始化签名器失败: %v", err))
	}

	return &Client{
		cfg:    cfg,
		signer: signer,
		httpCli: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: fmt.Sprintf("https://iaas.%s.oraclecloud.com", cfg.Region),
	}
}

// doRequest 内部通用请求方法
func (c *Client) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	var bodyBytes []byte
	var err error

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("序列化请求体失败: %w", err)
		}
	}

	url := c.baseURL + path
	req, err := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("构造 HTTP 请求失败: %w", err)
	}

	req.Host = req.URL.Host

	if err = c.signer.Sign(req, bodyBytes); err != nil {
		return nil, 0, fmt.Errorf("请求签名失败: %w", err)
	}

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应体失败: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// parseAPIError 从响应体解析 OCI API 错误
func parseAPIError(body []byte, statusCode int) *APIError {
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		apiErr.Code = "UnknownError"
		apiErr.Message = string(body)
	}
	apiErr.Status = statusCode
	return &apiErr
}

// doRequestAbsolute 使用完整 URL 发起请求（用于非 IAAS 端点，如 Identity API）
func (c *Client) doRequestAbsolute(method, fullURL string, body interface{}) ([]byte, int, error) {
	var bodyBytes []byte
	var err error

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("序列化请求体失败: %w", err)
		}
	}

	req, err := http.NewRequest(method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("构造 HTTP 请求失败: %w", err)
	}

	req.Host = req.URL.Host

	if err = c.signer.Sign(req, bodyBytes); err != nil {
		return nil, 0, fmt.Errorf("请求签名失败: %w", err)
	}

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应体失败: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// GetRegion 返回当前区域
func (c *Client) GetRegion() string {
	return c.cfg.Region
}

// GetTenancyOCID 返回 Tenancy OCID
func (c *Client) GetTenancyOCID() string {
	return c.cfg.TenancyOCID
}
