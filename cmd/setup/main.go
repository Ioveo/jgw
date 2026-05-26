// cmd/setup/main.go — 交互式初始化向导
// 用法: go run ./cmd/setup/   或   go build -o oci-setup.exe ./cmd/setup/
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"oci-grabber/internal/config"
	"oci-grabber/internal/keygen"
)

func main() {
	fmt.Println(`
╔══════════════════════════════════════════════════════╗
║    OCI 甲骨文抢机器 — 初始化配置向导                ║
║    只需 2 步手动操作，其余全部自动完成！             ║
╚══════════════════════════════════════════════════════╝
`)

	reader := bufio.NewReader(os.Stdin)
	prompt := func(label string) string {
		fmt.Printf("  ➤  %s: ", label)
		text, _ := reader.ReadString('\n')
		return strings.TrimSpace(text)
	}

	// ── Step 1: 生成密钥 ──────────────────────────────────────────────
	fmt.Println("【步骤 1/3】自动生成 RSA 4096 密钥对...")
	keyPath := "./oci_api_key.pem"
	result, err := keygen.GenerateKeyPair(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 生成密钥失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  ✓ 私钥已保存: %s\n", result.PrivateKeyPath)
	fmt.Printf("  ✓ 指纹已计算: %s\n\n", result.Fingerprint)

	// 显示需要上传的 API 公钥
	fmt.Println("━━━━━━━━━━━━━━━━━━ API 公钥（请完整复制） ━━━━━━━━━━━━━━━━━━")
	fmt.Println(result.APIPublicKeyPEM)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// ── Step 2: 指导用户到 OCI 控制台 ────────────────────────────────
	fmt.Println(`
【步骤 2/3】请完成以下 2 个手动操作（约 3 分钟）:

  ① 上传 API 公钥:
     打开浏览器 → 登录 https://cloud.oracle.com
     右上角头像 → "我的个人资料" → "API 密钥" → "添加 API 密钥"
     选择 "粘贴公钥" → 粘贴上面的 API 公钥全文 → 确认

  ② 复制 Tenancy OCID 和 User OCID:
     Tenancy OCID: 右上角头像 → "租户: xxx" → 复制 OCID
     User OCID:    右上角头像 → "我的个人资料" → 复制 OCID

  ③ 确认你的 Region (如 ap-tokyo-1 / ap-osaka-1 / ap-seoul-1)
`)

	fmt.Println("完成后请继续填写以下信息:")
	fmt.Println()

	// ── Step 3: 收集必填信息 ─────────────────────────────────────────
	tenancyOCID := prompt("Tenancy OCID")
	userOCID := prompt("User OCID")
	region := prompt("Region (如 ap-tokyo-1)")

	if tenancyOCID == "" || userOCID == "" || region == "" {
		fmt.Fprintln(os.Stderr, "❌ Tenancy OCID / User OCID / Region 不能为空")
		os.Exit(1)
	}

	fmt.Println("\n【步骤 3/3】生成配置文件 config.toml ...")

	cfg := config.Default()
	cfg.Auth.TenancyOCID = tenancyOCID
	cfg.Auth.UserOCID = userOCID
	cfg.Auth.Fingerprint = result.Fingerprint
	cfg.Auth.PrivateKeyPath = keyPath
	cfg.Auth.Region = region

	// CompartmentID 默认等于 Tenancy OCID（根 Compartment）
	cfg.Instance.CompartmentID = tenancyOCID
	cfg.Instance.SSHPublicKey = result.SSHPublicKey
	// ImageID 和 SubnetID 留空 → 程序启动时自动发现

	if err := config.Save("config.toml", cfg); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 写入 config.toml 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf(`
╔══════════════════════════════════════════════════════╗
║  ✅ 配置完成！                                       ║
╠══════════════════════════════════════════════════════╣
║  私钥文件:  %-41s ║
║  指  纹:    %-41s ║
║  Region:    %-41s ║
╠══════════════════════════════════════════════════════╣
║  现在可以运行: go run .                              ║
║  或双击 GUI: oci-grabber-gui.exe                     ║
╚══════════════════════════════════════════════════════╝
`, keyPath, result.Fingerprint, region)
}
