// Package keygen 自动生成 OCI API 密钥对、SSH 密钥和指纹
package keygen

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
)

// Result 密钥生成结果
type Result struct {
	PrivateKeyPath  string // 私钥文件路径
	APIPublicKeyPEM string // API 公钥 PEM（需上传到 OCI 控制台）
	SSHPublicKey    string // SSH 公钥（用于创建实例时注入）
	Fingerprint     string // OCI 格式指纹 xx:xx:...:xx（16 个 MD5 十六进制对）
}

// GenerateKeyPair 生成 RSA 4096 密钥对，保存私钥到 keyPath，同时返回 API 公钥、SSH 公钥和指纹
// 同一套密钥同时用于 OCI API 认证和 SSH 实例登录
func GenerateKeyPair(keyPath string) (*Result, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, fmt.Errorf("RSA 密钥生成失败: %w", err)
	}

	// 保存 PKCS1 格式私钥
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	if err := os.WriteFile(keyPath, privPEM, 0600); err != nil {
		return nil, fmt.Errorf("保存私钥文件失败: %w", err)
	}

	pub := &privKey.PublicKey

	// API 公钥 PEM（PKIX 格式，上传到 OCI 控制台）
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("序列化公钥失败: %w", err)
	}
	apiPubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	// OCI 指纹 = MD5(DER 公钥) 以冒号分隔的十六进制
	fingerprint := calcFingerprint(pubDER)

	// SSH 公钥（authorized_keys 格式）
	sshPub, err := marshalSSHPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("生成 SSH 公钥失败: %w", err)
	}

	return &Result{
		PrivateKeyPath:  keyPath,
		APIPublicKeyPEM: apiPubPEM,
		SSHPublicKey:    sshPub,
		Fingerprint:     fingerprint,
	}, nil
}

// CalcFingerprintFromFile 从已有私钥文件计算 OCI 指纹
func CalcFingerprintFromFile(keyPath string) (string, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("读取私钥文件失败: %w", err)
	}
	return CalcFingerprintFromPEM(data)
}

// CalcFingerprintFromPEM 从 PEM 字节计算 OCI 指纹
func CalcFingerprintFromPEM(privPEMBytes []byte) (string, error) {
	block, _ := pem.Decode(privPEMBytes)
	if block == nil {
		return "", fmt.Errorf("无效的 PEM 格式")
	}

	var pub *rsa.PublicKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("解析 PKCS1 私钥失败: %w", err)
		}
		pub = &priv.PublicKey
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("解析 PKCS8 私钥失败: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("私钥不是 RSA 类型")
		}
		pub = &rsaKey.PublicKey
	default:
		return "", fmt.Errorf("不支持的 PEM 类型: %s", block.Type)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	return calcFingerprint(pubDER), nil
}

// calcFingerprint 计算 DER 公钥的 MD5 指纹（OCI 格式）
func calcFingerprint(pubDER []byte) string {
	h := md5.Sum(pubDER)
	parts := make([]string, 16)
	for i, b := range h {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":")
}

// marshalSSHPublicKey 将 RSA 公钥转换为 authorized_keys 格式（不依赖 x/crypto）
func marshalSSHPublicKey(pub *rsa.PublicKey) (string, error) {
	var w bytes.Buffer

	writeBytes := func(b []byte) {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(b)))
		w.Write(lenBuf[:])
		w.Write(b)
	}

	writeMPInt := func(n *big.Int) {
		nb := n.Bytes()
		// mpint 要求最高位为 0（正数），若最高字节 MSB 置位则加前导 0x00
		if len(nb) == 0 || nb[0]&0x80 != 0 {
			nb = append([]byte{0x00}, nb...)
		}
		writeBytes(nb)
	}

	writeBytes([]byte("ssh-rsa"))
	writeMPInt(big.NewInt(int64(pub.E)))
	writeMPInt(pub.N)

	return "ssh-rsa " + base64.StdEncoding.EncodeToString(w.Bytes()) + " oci-grabber", nil
}
