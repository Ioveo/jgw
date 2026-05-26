// main.go — CLI 入口（使用共享 config 包）
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"oci-grabber/internal/config"
	"oci-grabber/internal/notify"
	"oci-grabber/internal/oci"
	"oci-grabber/internal/scheduler"
)

func main() {
	configPath := flag.String("config", "config.toml", "配置文件路径")
	flag.Parse()

	printBanner()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}
	if err := config.Validate(cfg); err != nil {
		log.Fatalf("[FATAL] 配置校验失败: %v", err)
	}
	if cfg.Scheduler.IntervalSeconds <= 0 {
		cfg.Scheduler.IntervalSeconds = 60
	}
	if cfg.Scheduler.MaxBackoffSeconds <= 0 {
		cfg.Scheduler.MaxBackoffSeconds = 600
	}

	notifier := notify.New(notify.Config{
		WebhookURL:       cfg.Notify.WebhookURL,
		TelegramBotToken: cfg.Notify.TelegramBotToken,
		TelegramChatID:   cfg.Notify.TelegramChatID,
	})

	client := oci.NewClient(oci.ClientConfig{
		TenancyOCID:    cfg.Auth.TenancyOCID,
		UserOCID:       cfg.Auth.UserOCID,
		Fingerprint:    cfg.Auth.Fingerprint,
		PrivateKeyPath: cfg.Auth.PrivateKeyPath,
		Region:         cfg.Auth.Region,
	})

	instanceCfg := oci.InstanceConfig{
		CompartmentID:      cfg.Instance.CompartmentID,
		AvailabilityDomain: cfg.Instance.AvailabilityDomain,
		DisplayName:        cfg.Instance.DisplayName,
		Shape:              cfg.Instance.Shape,
		OCPUs:              cfg.Instance.OCPUs,
		MemoryGB:           cfg.Instance.MemoryGB,
		ImageID:            cfg.Instance.ImageID,
		SubnetID:           cfg.Instance.SubnetID,
		AssignPublicIP:     cfg.Instance.AssignPublicIP,
		SSHPublicKey:       cfg.Instance.SSHPublicKey,
	}

	sched := scheduler.New(scheduler.Config{
		IntervalSeconds:   cfg.Scheduler.IntervalSeconds,
		JitterSeconds:     cfg.Scheduler.JitterSeconds,
		MaxBackoffSeconds: cfg.Scheduler.MaxBackoffSeconds,
		MaxAttempts:       cfg.Scheduler.MaxAttempts,
		Client:            client,
		InstanceConfig:    instanceCfg,
		Notifier:          notifier,
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("[INFO] 🚀 OCI 抢机器启动！Region: %s, Shape: %s (%.0f OCPU / %.0fGB)",
		cfg.Auth.Region, cfg.Instance.Shape, cfg.Instance.OCPUs, cfg.Instance.MemoryGB)

	go sched.Run()
	<-sigCh
	log.Println("[INFO] 收到退出信号，停止抢机...")
	sched.Stop()
}

func printBanner() {
	fmt.Println(`
  ██████╗  ██████╗██╗     ██████╗ ██████╗  █████╗ ██████╗ 
 ██╔═══██╗██╔════╝██║    ██╔════╝ ██╔══██╗██╔══██╗██╔══██╗
 ██║   ██║██║     ██║    ██║  ███╗██████╔╝███████║██████╔╝
 ██║   ██║██║     ██║    ██║   ██║██╔══██╗██╔══██║██╔══██╗
 ╚██████╔╝╚██████╗██║    ╚██████╔╝██║  ██║██║  ██║██████╔╝
  ╚═════╝  ╚═════╝╚═╝     ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ 
         OCI Instance Auto-Grabber  v1.0  CLI
`)
}
