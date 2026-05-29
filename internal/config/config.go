// Package config 定义共享配置结构及加载/保存工具
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Config 顶层配置
type Config struct {
	Auth      AuthConfig      `toml:"auth"`
	Instance  InstanceConfig  `toml:"instance"`
	Scheduler SchedulerConfig `toml:"scheduler"`
	Notify    NotifyConfig    `toml:"notify"`
}

// AuthConfig OCI 鉴权信息
type AuthConfig struct {
	TenancyOCID    string `toml:"tenancy_ocid"`
	UserOCID       string `toml:"user_ocid"`
	Fingerprint    string `toml:"fingerprint"`
	PrivateKeyPath string `toml:"private_key_path"`
	Region         string `toml:"region"`
}

// InstanceConfig 实例规格配置
type InstanceConfig struct {
	CompartmentID      string  `toml:"compartment_id"`
	AvailabilityDomain string  `toml:"availability_domain"`
	DisplayName        string  `toml:"display_name"`
	Shape              string  `toml:"shape"`
	OCPUs              float64 `toml:"ocpus"`
	MemoryGB           float64 `toml:"memory_gb"`
	ImageID            string  `toml:"image_id"`
	ImageFilter        string  `toml:"image_filter"`
	SubnetID           string  `toml:"subnet_id"`
	AssignPublicIP     bool    `toml:"assign_public_ip"`
	SSHPublicKey       string  `toml:"ssh_public_key"`
}

// SchedulerConfig 调度策略
type SchedulerConfig struct {
	IntervalSeconds   int `toml:"interval_seconds"`
	JitterSeconds     int `toml:"jitter_seconds"`
	MaxBackoffSeconds int `toml:"max_backoff_seconds"`
	MaxAttempts       int `toml:"max_attempts"`
}

// NotifyConfig 通知渠道
type NotifyConfig struct {
	WebhookURL       string `toml:"webhook_url"`
	TelegramBotToken string `toml:"telegram_bot_token"`
	TelegramChatID   string `toml:"telegram_chat_id"`
}

// Load 从 TOML 文件加载配置，若文件不存在，则尝试从环境变量读取
func Load(path string) (*Config, error) {
	var cfg Config
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		// 如果文件不存在，我们尝试全部使用环境变量进行兜底
		if os.IsNotExist(err) || path == "" {
			logEnv := func(k string) string {
				return os.Getenv(k)
			}
			cfg = *Default()
			cfg.Auth.TenancyOCID = logEnv("OCI_TENANCY_OCID")
			cfg.Auth.UserOCID = logEnv("OCI_USER_OCID")
			cfg.Auth.Fingerprint = logEnv("OCI_FINGERPRINT")
			cfg.Auth.PrivateKeyPath = logEnv("OCI_PRIVATE_KEY_PATH")
			cfg.Auth.Region = logEnv("OCI_REGION")

			cfg.Instance.CompartmentID = logEnv("OCI_COMPARTMENT_ID")
			cfg.Instance.AvailabilityDomain = logEnv("OCI_AVAILABILITY_DOMAIN")
			cfg.Instance.DisplayName = logEnv("OCI_DISPLAY_NAME")
			cfg.Instance.Shape = logEnv("OCI_SHAPE")
			cfg.Instance.ImageID = logEnv("OCI_IMAGE_ID")
			cfg.Instance.ImageFilter = logEnv("OCI_IMAGE_FILTER")
			cfg.Instance.SubnetID = logEnv("OCI_SUBNET_ID")
			cfg.Instance.SSHPublicKey = logEnv("OCI_SSH_PUBLIC_KEY")

			cfg.Notify.WebhookURL = logEnv("OCI_WEBHOOK_URL")
			cfg.Notify.TelegramBotToken = logEnv("OCI_TG_BOT_TOKEN")
			cfg.Notify.TelegramChatID = logEnv("OCI_TG_CHAT_ID")

			// 解析整型和浮点型环境变量
			parseIntEnv := func(k string, def int) int {
				val := logEnv(k)
				if val == "" {
					return def
				}
				var i int
				if _, err := fmt.Sscanf(val, "%d", &i); err != nil {
					return def
				}
				return i
			}
			parseFloatEnv := func(k string, def float64) float64 {
				val := logEnv(k)
				if val == "" {
					return def
				}
				var f float64
				if _, err := fmt.Sscanf(val, "%f", &f); err != nil {
					return def
				}
				return f
			}

			cfg.Instance.OCPUs = parseFloatEnv("OCI_OCPUS", 4)
			cfg.Instance.MemoryGB = parseFloatEnv("OCI_MEMORY_GB", 24)

			// 解析布尔型环境变量
			parseBoolEnv := func(k string, def bool) bool {
				val := logEnv(k)
				if val == "" {
					return def
				}
				b, err := strconv.ParseBool(val)
				if err != nil {
					return def
				}
				return b
			}
			cfg.Instance.AssignPublicIP = parseBoolEnv("OCI_ASSIGN_PUBLIC_IP", true)

			cfg.Scheduler.IntervalSeconds = parseIntEnv("OCI_INTERVAL_SECONDS", 60)
			cfg.Scheduler.JitterSeconds = parseIntEnv("OCI_JITTER_SECONDS", 15)
			cfg.Scheduler.MaxBackoffSeconds = parseIntEnv("OCI_MAX_BACKOFF_SECONDS", 600)
			cfg.Scheduler.MaxAttempts = parseIntEnv("OCI_MAX_ATTEMPTS", 0)

			// Bug修复: OCI_PRIVATE_KEY_CONTENT 写入失败时明确报错（之前静默忽略会导致鉴权失败）
			pkContent := os.Getenv("OCI_PRIVATE_KEY_CONTENT")
			if pkContent != "" {
				tmpKeyPath := "./oci_private_key_temp.pem"
				if writeErr := os.WriteFile(tmpKeyPath, []byte(pkContent), 0600); writeErr != nil {
					return nil, fmt.Errorf("写入临时私钥文件失败: %w", writeErr)
				}
				cfg.Auth.PrivateKeyPath = tmpKeyPath
			}

			// 如果核心环境变量为空，则真的返回错误
			if cfg.Auth.TenancyOCID == "" {
				return nil, fmt.Errorf("未找到配置文件，且环境变量 OCI_TENANCY_OCID 为空")
			}
			return &cfg, nil
		}
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	return &cfg, nil
}

// Save 将配置写入 TOML 文件
func Save(path string, cfg *Config) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建配置文件失败: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

// Default 返回带默认值的配置
func Default() *Config {
	return &Config{
		Instance: InstanceConfig{
			DisplayName: "free-arm-01",
			Shape:       "VM.Standard.A1.Flex",
			OCPUs:       4,
			MemoryGB:    24,
		},
		Scheduler: SchedulerConfig{
			IntervalSeconds:   60,
			JitterSeconds:     15,
			MaxBackoffSeconds: 600,
			MaxAttempts:       0,
		},
	}
}

// Validate 校验必填项 (支持空 ImageID/SubnetID 进行自动发现)
func Validate(cfg *Config) error {
	switch {
	case cfg.Auth.TenancyOCID == "":
		return fmt.Errorf("auth.tenancy_ocid 不能为空")
	case cfg.Auth.UserOCID == "":
		return fmt.Errorf("auth.user_ocid 不能为空")
	case cfg.Auth.Fingerprint == "":
		return fmt.Errorf("auth.fingerprint 不能为空")
	case cfg.Auth.PrivateKeyPath == "":
		return fmt.Errorf("auth.private_key_path 不能为空")
	case cfg.Auth.Region == "":
		return fmt.Errorf("auth.region 不能为空")
	case cfg.Instance.CompartmentID == "":
		return fmt.Errorf("instance.compartment_id 不能为空")
	case cfg.Instance.SSHPublicKey == "":
		return fmt.Errorf("instance.ssh_public_key 不能为空")
	}
	return nil
}
