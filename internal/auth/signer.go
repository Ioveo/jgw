// Package auth 实现 OCI HTTP Signature v1 请求签名
// 参考: https://docs.oracle.com/en-us/iaas/Content/API/Concepts/signingrequests.htm
package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Signer OCI HTTP Signature 签名器
type Signer struct {
	tenancyOCID string
	userOCID    string
	fingerprint string
	privateKey  *rsa.PrivateKey
	keyID       string // "tenancy/user/fingerprint"
}

// NewSigner 从 PEM 文件创建签名器
func NewSigner(tenancyOCID, userOCID, fingerprint, privateKeyPath string) (*Signer, error) {
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("读取私钥文件失败: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("私钥 PEM 解码失败，请检查文件格式")
	}

	var privateKey *rsa.PrivateKey

	switch block.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析 PKCS1 私钥失败: %w", err)
		}
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析 PKCS8 私钥失败: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("私钥不是 RSA 类型")
		}
	default:
		return nil, fmt.Errorf("不支持的 PEM 块类型: %s", block.Type)
	}

	keyID := fmt.Sprintf("%s/%s/%s", tenancyOCID, userOCID, fingerprint)

	return &Signer{
		tenancyOCID: tenancyOCID,
		userOCID:    userOCID,
		fingerprint: fingerprint,
		privateKey:  privateKey,
		keyID:       keyID,
	}, nil
}

// Sign 对 HTTP 请求进行 OCI Signature 签名（修改请求头）
// 对于 POST/PUT 请求，body 内容的 SHA256 需提前写入 x-content-sha256 和 Content-Length
func (s *Signer) Sign(req *http.Request, bodyBytes []byte) error {
	// RFC1123 时间格式
	dateStr := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("Date", dateStr)
	req.Header.Set("Host", req.Host)

	// 签名头列表
	var signedHeaders []string
	var signingParts []string

	isBody := req.Method == http.MethodPost || req.Method == http.MethodPut || req.Method == http.MethodPatch

	// 必须签名的基础头
	baseHeaders := []string{"date", "host", "(request-target)"}

	for _, h := range baseHeaders {
		switch h {
		case "(request-target)":
			path := req.URL.RequestURI()
			signingParts = append(signingParts, fmt.Sprintf("(request-target): %s %s",
				strings.ToLower(req.Method), path))
		default:
			signingParts = append(signingParts, fmt.Sprintf("%s: %s", h, req.Header.Get(toTitleCase(h))))
		}
		signedHeaders = append(signedHeaders, h)
	}

	// 有请求体时额外签名
	if isBody {
		bodyHash := sha256.Sum256(bodyBytes)
		bodyHashB64 := base64.StdEncoding.EncodeToString(bodyHash[:])
		req.Header.Set("x-content-sha256", bodyHashB64)
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")

		bodyHeaders := []string{"content-type", "content-length", "x-content-sha256"}
		for _, h := range bodyHeaders {
			signingParts = append(signingParts, fmt.Sprintf("%s: %s", h, req.Header.Get(toTitleCase(h))))
			signedHeaders = append(signedHeaders, h)
		}
	}

	signingString := strings.Join(signingParts, "\n")

	// RSA-SHA256 签名
	hashed := sha256.Sum256([]byte(signingString))
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return fmt.Errorf("RSA 签名失败: %w", err)
	}

	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	authHeader := fmt.Sprintf(
		`Signature version="1",keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		s.keyID,
		strings.Join(signedHeaders, " "),
		sigB64,
	)
	req.Header.Set("Authorization", authHeader)

	return nil
}

// toTitleCase 将小写头名转换为首字母大写（HTTP 标准格式）
func toTitleCase(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}
