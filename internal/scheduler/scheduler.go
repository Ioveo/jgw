// Package scheduler 实现抢机轮询调度与退避策略
package scheduler

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"oci-grabber/internal/notify"
	"oci-grabber/internal/oci"
)

// Config 调度器配置
type Config struct {
	IntervalSeconds   int
	JitterSeconds     int
	MaxBackoffSeconds int
	MaxAttempts       int
	Client            *oci.Client
	InstanceConfig    oci.InstanceConfig
	Notifier          *notify.Notifier
}

// Scheduler 抢机调度器
type Scheduler struct {
	cfg          Config
	stopCh       chan struct{}
	attempts     int64
	consecutive  int64 // 连续失败次数（用于退避计算）
	ads          []string  // 可用域列表
	countdown    int64 // 剩余等待秒数
}

// New 创建调度器
func New(cfg Config) *Scheduler {
	return &Scheduler{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Run 开始调度（阻塞运行）
func (s *Scheduler) Run() {
	// 动态识别镜像与网络
	if err := s.initMetadata(); err != nil {
		log.Printf("[FATAL] 动态初始化镜像/网络失败: %v", err)
		s.cfg.Notifier.Send("❌ OCI 初始化失败", err.Error())
		return
	}

	// 初始化可用域列表
	s.initADs()


	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		totalAttempts := atomic.AddInt64(&s.attempts, 1)

		// 最大尝试次数检查
		if s.cfg.MaxAttempts > 0 && totalAttempts > int64(s.cfg.MaxAttempts) {
			log.Printf("[INFO] 已达最大尝试次数 %d，停止抢机", s.cfg.MaxAttempts)
			return
		}

		log.Printf("[INFO] ⏳ 第 %d 次尝试...", totalAttempts)

		success := s.tryLaunch()
		if success {
			return
		}

		// 计算下次等待时间
		waitSec := s.calcWait()
		log.Printf("[INFO] 等待 %ds 后重试...\n", waitSec)

		// 倒计时更新逻辑
		atomic.StoreInt64(&s.countdown, int64(waitSec))
		ticker := time.NewTicker(time.Second)
		
		stopWait := false
		for wait := waitSec; wait > 0 && !stopWait; {
			select {
			case <-s.stopCh:
				stopWait = true
			case <-ticker.C:
				wait--
				atomic.StoreInt64(&s.countdown, int64(wait))
			}
		}
		ticker.Stop()
		atomic.StoreInt64(&s.countdown, 0)

		select {
		case <-s.stopCh:
			return
		default:
		}
	}
}

// Countdown 返回当前剩余等待秒数
func (s *Scheduler) Countdown() int {
	return int(atomic.LoadInt64(&s.countdown))
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// Attempts 返回当前总尝试次数（供 GUI 显示）
func (s *Scheduler) Attempts() int64 {
	return atomic.LoadInt64(&s.attempts)
}

// initADs 初始化可用域，若配置了固定 AD 则直接使用
func (s *Scheduler) initADs() {
	if s.cfg.InstanceConfig.AvailabilityDomain != "" {
		s.ads = []string{s.cfg.InstanceConfig.AvailabilityDomain}
		log.Printf("[INFO] 使用指定可用域: %s", s.cfg.InstanceConfig.AvailabilityDomain)
		return
	}

	log.Printf("[INFO] 正在获取 %s 区域可用域列表...", s.cfg.Client.GetRegion())
	adList, apiErr := s.cfg.Client.ListAvailabilityDomains(s.cfg.InstanceConfig.CompartmentID)
	if apiErr != nil {
		log.Printf("[WARN] 获取可用域失败: %v，将使用默认 AD-1", apiErr)
		// 使用常见命名格式作为回退
		s.ads = []string{fmt.Sprintf("AD-1")}
		return
	}

	for _, ad := range adList {
		s.ads = append(s.ads, ad.Name)
	}
	log.Printf("[INFO] 发现 %d 个可用域: %v", len(s.ads), s.ads)
}

// tryLaunch 尝试在所有 AD 上创建实例，任一成功则返回 true
func (s *Scheduler) tryLaunch() bool {
	for _, ad := range s.ads {
		select {
		case <-s.stopCh:
			return false
		default:
		}

		log.Printf("[INFO] 🎯 尝试 AD: %s", ad)
		inst, apiErr := s.cfg.Client.LaunchInstance(s.cfg.InstanceConfig, ad)

		if apiErr == nil {
			// 🎉 成功！
			atomic.StoreInt64(&s.consecutive, 0)
			msg := fmt.Sprintf("🎉 抢机成功！\n实例ID: %s\n名称: %s\n状态: %s\n可用域: %s\n创建时间: %s",
				inst.ID, inst.DisplayName, inst.LifecycleState, inst.AvailabilityDomain, inst.TimeCreated)

			log.Printf("[SUCCESS] %s", msg)
			s.cfg.Notifier.Send("✅ OCI 抢机成功！", msg)
			return true
		}

		// 分类处理错误
		switch {
		case apiErr.IsAuthError():
			log.Printf("[ERROR] ❌ 鉴权失败 (code=%s): %s\n请检查 OCID / 私钥 / Fingerprint 配置", apiErr.Code, apiErr.Message)
			s.cfg.Notifier.Send("❌ OCI 鉴权失败", apiErr.Message)
			// 鉴权失败无需重试，直接退出
			close(s.stopCh)
			return false

		case apiErr.IsThrottled():
			log.Printf("[WARN] ⚠️  触发限流 (429)，跳过当前 AD: %s", ad)
			atomic.AddInt64(&s.consecutive, 3) // 触发限流时加大惩罚度，使退避时间快速拉长

		case apiErr.IsOutOfCapacity():
			log.Printf("[INFO] 💤 容量不足 (AD=%s): %s", ad, apiErr.Message)
			atomic.AddInt64(&s.consecutive, 1)

		default:
			log.Printf("[WARN] ⚠️  未知错误 (code=%s, status=%d): %s", apiErr.Code, apiErr.Status, apiErr.Message)
			atomic.AddInt64(&s.consecutive, 1)
		}
	}
	return false
}

// calcWait 根据连续失败次数计算退避等待时间（含随机抖动）
func (s *Scheduler) calcWait() int {
	consecutive := atomic.LoadInt64(&s.consecutive)
	base := float64(s.cfg.IntervalSeconds)

	// 指数退避：base * 2^(consecutive/5)，每 5 次失败翻倍
	backoff := base * math.Pow(2, float64(consecutive)/5)
	maxBackoff := float64(s.cfg.MaxBackoffSeconds)
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// 添加随机抖动 [-jitter, +jitter]
	jitter := rand.Float64()*float64(s.cfg.JitterSeconds*2) - float64(s.cfg.JitterSeconds)
	wait := int(backoff + jitter)
	if wait < 1 {
		wait = 1
	}
	return wait
}

// initMetadata 动态查找并填充空缺的 ImageID 和 SubnetID
func (s *Scheduler) initMetadata() error {
	compID := s.cfg.InstanceConfig.CompartmentID
	shape := s.cfg.InstanceConfig.Shape

	// 1. 如果没有指定 ImageID，则动态搜寻符合条件的镜像（支持自定义过滤器如 "ubuntu 22.04"）
	if s.cfg.InstanceConfig.ImageID == "" {
		filter := strings.ToLower(s.cfg.InstanceConfig.ImageFilter)
		if filter == "" {
			filter = "ubuntu"
		}
		log.Printf("[INFO] 🔍 检测到 ImageID 为空，开始自动适配 '%s' 且包含关键字 '%s' 的镜像...", shape, filter)
		images, apiErr := s.cfg.Client.ListImages(compID, shape)
		if apiErr != nil {
			return fmt.Errorf("动态获取镜像列表失败: %v", apiErr)
		}

		var targetImage *oci.ImageInfo
		for _, img := range images {
			displayLower := strings.ToLower(img.DisplayName)
			if img.LifecycleState != "AVAILABLE" {
				continue
			}

			// 匹配过滤关键字（支持多关键字空格分隔，如 "ubuntu 22.04"）
			matched := true
			for _, part := range strings.Fields(filter) {
				if !strings.Contains(displayLower, part) {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}

			// A1 运行 ARM 需要 aarch64/arm64 架构镜像，同时兼容 oracle 不同命名规范
			if strings.Contains(shape, "A1") {
				isArm := strings.Contains(displayLower, "aarch64") ||
					strings.Contains(displayLower, "arm64") ||
					strings.Contains(displayLower, "arm")
				if !isArm {
					continue
				}
			}
			// 拷贝循环变量再取地址，避免 Go 1.22 以前循环变量复用问题
			imgCopy := img
			targetImage = &imgCopy
			break
		}

		if targetImage == nil {
			return fmt.Errorf("在当前 Region (%s) 未找到匹配 '%s' 的镜像，请调整 image_filter 或在配置中手动指定 image_id", s.cfg.Client.GetRegion(), filter)
		}

		s.cfg.InstanceConfig.ImageID = targetImage.ID
		log.Printf("[INFO] ✓ 自动匹配成功镜像: %s (ID: %s)", targetImage.DisplayName, targetImage.ID)
	}

	// 2. 如果没有指定 SubnetID，则动态搜寻第一个可用的 VCN 和子网
	if s.cfg.InstanceConfig.SubnetID == "" {
		log.Printf("[INFO] 🔍 检测到 SubnetID 为空，开始自动适配 Compartment 网络...")
		vcns, apiErr := s.cfg.Client.ListVcns(compID)
		if apiErr != nil {
			return fmt.Errorf("动态获取 VCN 列表失败: %v", apiErr)
		}

		if len(vcns) == 0 {
			return fmt.Errorf("当前 Compartment 内无虚拟云网络 (VCN)，请先登录控制台创建一个 VCN 并生成子网")
		}

		// 选用第一个 AVAILABLE 的 VCN
		var targetVcn *oci.VcnInfo
		for _, v := range vcns {
			if v.LifecycleState == "AVAILABLE" {
				vCopy := v // 拷贝后取地址，避免循环变量复用问题
				targetVcn = &vCopy
				break
			}
		}

		if targetVcn == nil {
			return fmt.Errorf("当前 Compartment 无可用的 VCN，请检查网络状态")
		}

		log.Printf("[INFO] 找到可用网络 VCN: %s (ID: %s)，正在查询子网...", targetVcn.DisplayName, targetVcn.ID)

		subnets, apiErr := s.cfg.Client.ListSubnets(compID, targetVcn.ID)
		if apiErr != nil {
			return fmt.Errorf("动态获取子网列表失败: %v", apiErr)
		}

		var targetSubnet *oci.SubnetInfo
		for _, sub := range subnets {
			if sub.LifecycleState == "AVAILABLE" {
				subCopy := sub // 拷贝后取地址
				targetSubnet = &subCopy
				break
			}
		}

		if targetSubnet == nil {
			return fmt.Errorf("VCN (%s) 下没有可用子网，请先在该 VCN 下创建子网", targetVcn.DisplayName)
		}

		s.cfg.InstanceConfig.SubnetID = targetSubnet.ID
		log.Printf("[INFO] ✓ 自动匹配成功子网: %s (ID: %s)", targetSubnet.DisplayName, targetSubnet.ID)
	}

	return nil
}

