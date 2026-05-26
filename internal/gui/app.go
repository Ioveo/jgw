// Package gui — 主 GUI 应用逻辑
package gui

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/BurntSushi/toml"

	"oci-grabber/internal/config"
	"oci-grabber/internal/keygen"
	"oci-grabber/internal/notify"
	"oci-grabber/internal/oci"
	"oci-grabber/internal/scheduler"
)

// logWriter 将 log 包输出重定向到 channel
type logWriter struct {
	ch  chan string
	out io.Writer // 同时写原始输出（stderr）
}

func (w *logWriter) Write(p []byte) (int, error) {
	select {
	case w.ch <- string(p):
	default:
	}
	if w.out != nil {
		_, _ = w.out.Write(p)
	}
	return len(p), nil
}

// ──────────────────────────────────────────────
// GUIApp 主应用结构
// ──────────────────────────────────────────────

// GUIApp 持有所有 UI 组件与运行时状态
type GUIApp struct {
	win fyne.Window

	// ── Auth 标签页 ──
	tenancyOCID   *widget.Entry
	userOCID      *widget.Entry
	fingerprint   *widget.Entry
	privateKeyPath *widget.Entry
	regionSelect  *widget.Select

	// ── Instance 标签页 ──
	compartmentID  *widget.Entry
	adEntry        *widget.Entry
	displayName    *widget.Entry
	shapeSelect    *widget.Select
	ocpus          *widget.Entry
	memoryGB       *widget.Entry
	imageID        *widget.Entry
	subnetID       *widget.Entry
	sshKey         *widget.Entry
	assignPublicIP *widget.Check

	// ── Scheduler 标签页 ──
	interval   *widget.Entry
	jitter     *widget.Entry
	maxBackoff *widget.Entry
	maxAttempts *widget.Entry

	// ── Notify 标签页 ──
	webhookURL   *widget.Entry
	tgBotToken   *widget.Entry
	tgChatID     *widget.Entry

	// ── 日志 & 状态 ──
	logEntry      *widget.Entry
	statusLabel   *widget.Label
	attemptsLabel *widget.Label
	startBtn      *widget.Button
	stopBtn       *widget.Button

	// ── 运行时 ──
	mu      sync.Mutex
	running bool
	sched   *scheduler.Scheduler
	logCh  chan string
}

// NewGUIApp 创建 GUI 应用实例
func NewGUIApp(win fyne.Window) *GUIApp {
	return &GUIApp{
		win:   win,
		logCh: make(chan string, 300),
	}
}

// Build 构建并返回根 UI 组件
func (g *GUIApp) Build() fyne.CanvasObject {
	g.initWidgets()

	// 重定向 log 到 GUI channel（同时保留 stderr）
	log.SetOutput(&logWriter{ch: g.logCh, out: os.Stderr})
	log.SetFlags(log.Ltime)

	// 尝试加载默认配置
	if cfg, err := config.Load("config.toml"); err == nil {
		g.applyConfig(cfg)
	} else {
		g.applyConfig(config.Default())
	}

	go g.drainLogs()

	// ── 配置区（左侧） ──
	tabs := container.NewAppTabs(
		container.NewTabItem("🔑 认证", g.buildAuthTab()),
		container.NewTabItem("💻 实例", g.buildInstanceTab()),
		container.NewTabItem("⏱ 调度", g.buildSchedulerTab()),
		container.NewTabItem("🔔 通知", g.buildNotifyTab()),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	// ── 日志区（右侧） ──
	g.logEntry = widget.NewMultiLineEntry()
	g.logEntry.Disable()
	g.logEntry.TextStyle = fyne.TextStyle{Monospace: true}
	g.logEntry.SetPlaceHolder("日志将在此实时显示...")
	logScroll := container.NewScroll(g.logEntry)

	g.statusLabel = widget.NewLabelWithStyle("● 就绪", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	g.attemptsLabel = widget.NewLabel("尝试次数: 0")

	statusRow := container.NewHBox(g.statusLabel, layout.NewSpacer(), g.attemptsLabel)

	logPanel := container.NewBorder(
		widget.NewLabelWithStyle("📋 实时日志", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		statusRow,
		nil, nil,
		logScroll,
	)

	// ── 主分割 ──
	split := container.NewHSplit(tabs, logPanel)
	split.Offset = 0.44

	// ── 底部工具栏 ──
	saveBtn := widget.NewButtonWithIcon("保存配置", theme.DocumentSaveIcon(), func() {
		cfg := g.collectConfig()
		if err := config.Save("config.toml", cfg); err != nil {
			dialog.ShowError(err, g.win)
			return
		}
		dialog.ShowInformation("成功", "配置已保存到 config.toml", g.win)
	})

	loadBtn := widget.NewButtonWithIcon("加载配置", theme.FolderOpenIcon(), func() {
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			defer r.Close()
			var cfg config.Config
			if _, err2 := toml.NewDecoder(r).Decode(&cfg); err2 != nil {
				dialog.ShowError(err2, g.win)
				return
			}
			g.applyConfig(&cfg)
		}, g.win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".toml"}))
		fd.Show()
	})

	clearBtn := widget.NewButtonWithIcon("清空日志", theme.DeleteIcon(), func() {
		g.logEntry.SetText("")
	})

	g.startBtn = widget.NewButtonWithIcon("▶  开始抢机", theme.MediaPlayIcon(), g.startGrabbing)
	g.startBtn.Importance = widget.HighImportance

	g.stopBtn = widget.NewButtonWithIcon("■  停止", theme.MediaStopIcon(), g.stopGrabbing)
	g.stopBtn.Disable()

	toolbar := container.NewHBox(
		saveBtn, loadBtn,
		layout.NewSpacer(),
		clearBtn, g.startBtn, g.stopBtn,
	)

	return container.NewBorder(nil, toolbar, nil, nil, split)
}

// ──────────────────────────────────────────────
// 标签页构建
// ──────────────────────────────────────────────

func (g *GUIApp) buildAuthTab() fyne.CanvasObject {
	// ── 一键生成密钥按钮 ──
	genBtn := widget.NewButtonWithIcon("🔑 一键生成密钥", theme.ContentAddIcon(), func() {
		go func() {
			result, err := keygen.GenerateKeyPair("./oci_api_key.pem")
			if err != nil {
				dialog.ShowError(fmt.Errorf("密钥生成失败: %v", err), g.win)
				return
			}
			// 自动填入各字段
			g.fingerprint.SetText(result.Fingerprint)
			g.privateKeyPath.SetText(result.PrivateKeyPath)
			g.sshKey.SetText(result.SSHPublicKey)

			// 弹窗显示需要上传到 OCI 控制台的 API 公钥
			msg := fmt.Sprintf(
				"密钥已生成！请完成以下 2 步：\n\n"+
					"① 登录 https://cloud.oracle.com\n"+
					"② 右上角头像 → 我的个人资料\n"+
					"   → API 密钥 → 添加 API 密钥\n"+
					"   → 粘贴公钥 → 复制粘贴下面的内容\n\n"+
					"────────────── API 公钥 ──────────────\n"+
					"%s\n"+
					"─────────────────────────────────────\n\n"+
					"指纹（已自动填入）: %s\n"+
					"私钥路径（已自动填入）: %s",
				result.APIPublicKeyPEM,
				result.Fingerprint,
				result.PrivateKeyPath,
			)
			dialog.ShowInformation("✅ 密钥生成成功", msg, g.win)
		}()
	})
	genBtn.Importance = widget.WarningImportance

	// ── 若已有私钥则计算指纹 ──
	calcBtn := widget.NewButton("从已有私钥计算指纹", func() {
		keyPath := strings.TrimSpace(g.privateKeyPath.Text)
		if keyPath == "" {
			dialog.ShowError(fmt.Errorf("请先填写或选择私钥路径"), g.win)
			return
		}
		fp, err := keygen.CalcFingerprintFromFile(keyPath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("计算指纹失败: %v", err), g.win)
			return
		}
		g.fingerprint.SetText(fp)
		dialog.ShowInformation("成功", "指纹已自动计算并填入", g.win)
	})

	// ── 私钥路径 + 浏览按钮 ──
	browseBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			r.Close()
			g.privateKeyPath.SetText(r.URI().Path())
		}, g.win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".pem"}))
		fd.Show()
	})
	keyRow := container.NewBorder(nil, nil, nil, browseBtn, g.privateKeyPath)

	// ── 说明标签 ──
	hint := widget.NewLabelWithStyle(
		"Tenancy OCID: 登录控制台 → 右上角头像 → 租户信息\nUser OCID:    右上角头像 → 我的个人资料",
		fyne.TextAlignLeading,
		fyne.TextStyle{Italic: true},
	)

	form := widget.NewForm(
		widget.NewFormItem("Tenancy OCID", g.tenancyOCID),
		widget.NewFormItem("User OCID", g.userOCID),
		widget.NewFormItem("Fingerprint", g.fingerprint),
		widget.NewFormItem("私钥路径 (.pem)", keyRow),
		widget.NewFormItem("区域 (Region)", g.regionSelect),
	)

	btnRow := container.NewHBox(genBtn, calcBtn)

	return container.NewScroll(container.NewVBox(hint, btnRow, form))
}

func (g *GUIApp) buildInstanceTab() fyne.CanvasObject {
	return container.NewScroll(widget.NewForm(
		widget.NewFormItem("Compartment ID", g.compartmentID),
		widget.NewFormItem("可用域 (留空=自动)", g.adEntry),
		widget.NewFormItem("实例名称", g.displayName),
		widget.NewFormItem("规格 (Shape)", g.shapeSelect),
		widget.NewFormItem("OCPU 数量", g.ocpus),
		widget.NewFormItem("内存 (GB)", g.memoryGB),
		widget.NewFormItem("镜像 ID", g.imageID),
		widget.NewFormItem("子网 ID", g.subnetID),
		widget.NewFormItem("SSH 公钥", g.sshKey),
		widget.NewFormItem("分配公网 IP", g.assignPublicIP),
	))
}

func (g *GUIApp) buildSchedulerTab() fyne.CanvasObject {
	return container.NewScroll(widget.NewForm(
		widget.NewFormItem("轮询间隔 (秒)", g.interval),
		widget.NewFormItem("随机抖动 (秒)", g.jitter),
		widget.NewFormItem("最大退避 (秒)", g.maxBackoff),
		widget.NewFormItem("最大尝试 (0=无限)", g.maxAttempts),
	))
}

func (g *GUIApp) buildNotifyTab() fyne.CanvasObject {
	return container.NewScroll(widget.NewForm(
		widget.NewFormItem("Webhook URL", g.webhookURL),
		widget.NewFormItem("Telegram Bot Token", g.tgBotToken),
		widget.NewFormItem("Telegram Chat ID", g.tgChatID),
	))
}

// ──────────────────────────────────────────────
// Widget 初始化
// ──────────────────────────────────────────────

var ociRegions = []string{
	"ap-tokyo-1", "ap-osaka-1", "ap-seoul-1", "ap-singapore-1",
	"ap-sydney-1", "ap-mumbai-1", "us-ashburn-1", "us-phoenix-1",
	"eu-frankfurt-1", "uk-london-1", "ca-toronto-1", "sa-saopaulo-1",
}

var ociShapes = []string{
	"VM.Standard.A1.Flex",
	"VM.Standard.E2.1.Micro",
	"VM.Standard3.Flex",
	"VM.Standard.E4.Flex",
}

func (g *GUIApp) initWidgets() {
	// Auth
	g.tenancyOCID = widget.NewEntry()
	g.tenancyOCID.SetPlaceHolder("ocid1.tenancy.oc1..aaaaaa...")
	g.userOCID = widget.NewEntry()
	g.userOCID.SetPlaceHolder("ocid1.user.oc1..aaaaaa...")
	g.fingerprint = widget.NewEntry()
	g.fingerprint.SetPlaceHolder("xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx")
	g.privateKeyPath = widget.NewEntry()
	g.privateKeyPath.SetPlaceHolder("./oci_private_key.pem")
	g.regionSelect = widget.NewSelect(ociRegions, nil)
	g.regionSelect.SetSelected("ap-tokyo-1")

	// Instance
	g.compartmentID = widget.NewEntry()
	g.compartmentID.SetPlaceHolder("ocid1.compartment.oc1..aaaaaa...（通常与 Tenancy OCID 相同）")
	g.adEntry = widget.NewEntry()
	g.adEntry.SetPlaceHolder("留空则自动轮询所有可用域")
	g.displayName = widget.NewEntry()
	g.displayName.SetText("free-arm-01")
	g.shapeSelect = widget.NewSelect(ociShapes, nil)
	g.shapeSelect.SetSelected("VM.Standard.A1.Flex")
	g.ocpus = widget.NewEntry()
	g.ocpus.SetText("4")
	g.memoryGB = widget.NewEntry()
	g.memoryGB.SetText("24")
	g.imageID = widget.NewEntry()
	g.imageID.SetPlaceHolder("ocid1.image.oc1.ap-tokyo-1.aaaaa...")
	g.subnetID = widget.NewEntry()
	g.subnetID.SetPlaceHolder("ocid1.subnet.oc1.ap-tokyo-1.aaaaa...")
	g.sshKey = widget.NewMultiLineEntry()
	g.sshKey.SetPlaceHolder("ssh-rsa AAAA...")
	g.sshKey.SetMinRowsVisible(3)
	g.assignPublicIP = widget.NewCheck("", nil)
	g.assignPublicIP.SetChecked(true)

	// Scheduler
	g.interval = widget.NewEntry()
	g.interval.SetText("60")
	g.jitter = widget.NewEntry()
	g.jitter.SetText("15")
	g.maxBackoff = widget.NewEntry()
	g.maxBackoff.SetText("600")
	g.maxAttempts = widget.NewEntry()
	g.maxAttempts.SetText("0")

	// Notify
	g.webhookURL = widget.NewEntry()
	g.webhookURL.SetPlaceHolder("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...")
	g.tgBotToken = widget.NewEntry()
	g.tgBotToken.SetPlaceHolder("123456:ABC-DEF...")
	g.tgChatID = widget.NewEntry()
	g.tgChatID.SetPlaceHolder("-100123456789")
}

// ──────────────────────────────────────────────
// 配置 apply / collect
// ──────────────────────────────────────────────

func (g *GUIApp) applyConfig(cfg *config.Config) {
	g.tenancyOCID.SetText(cfg.Auth.TenancyOCID)
	g.userOCID.SetText(cfg.Auth.UserOCID)
	g.fingerprint.SetText(cfg.Auth.Fingerprint)
	g.privateKeyPath.SetText(cfg.Auth.PrivateKeyPath)
	if cfg.Auth.Region != "" {
		g.regionSelect.SetSelected(cfg.Auth.Region)
	}

	g.compartmentID.SetText(cfg.Instance.CompartmentID)
	g.adEntry.SetText(cfg.Instance.AvailabilityDomain)
	g.displayName.SetText(cfg.Instance.DisplayName)
	if cfg.Instance.Shape != "" {
		g.shapeSelect.SetSelected(cfg.Instance.Shape)
	}
	g.ocpus.SetText(fmt.Sprintf("%.0f", cfg.Instance.OCPUs))
	g.memoryGB.SetText(fmt.Sprintf("%.0f", cfg.Instance.MemoryGB))
	g.imageID.SetText(cfg.Instance.ImageID)
	g.subnetID.SetText(cfg.Instance.SubnetID)
	g.sshKey.SetText(cfg.Instance.SSHPublicKey)
	g.assignPublicIP.SetChecked(cfg.Instance.AssignPublicIP)

	g.interval.SetText(strconv.Itoa(cfg.Scheduler.IntervalSeconds))
	g.jitter.SetText(strconv.Itoa(cfg.Scheduler.JitterSeconds))
	g.maxBackoff.SetText(strconv.Itoa(cfg.Scheduler.MaxBackoffSeconds))
	g.maxAttempts.SetText(strconv.Itoa(cfg.Scheduler.MaxAttempts))

	g.webhookURL.SetText(cfg.Notify.WebhookURL)
	g.tgBotToken.SetText(cfg.Notify.TelegramBotToken)
	g.tgChatID.SetText(cfg.Notify.TelegramChatID)
}

func (g *GUIApp) collectConfig() *config.Config {
	parseInt := func(s string, def int) int {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return def
		}
		return v
	}
	parseFloat := func(s string, def float64) float64 {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return def
		}
		return v
	}

	return &config.Config{
		Auth: config.AuthConfig{
			TenancyOCID:    strings.TrimSpace(g.tenancyOCID.Text),
			UserOCID:       strings.TrimSpace(g.userOCID.Text),
			Fingerprint:    strings.TrimSpace(g.fingerprint.Text),
			PrivateKeyPath: strings.TrimSpace(g.privateKeyPath.Text),
			Region:         g.regionSelect.Selected,
		},
		Instance: config.InstanceConfig{
			CompartmentID:      strings.TrimSpace(g.compartmentID.Text),
			AvailabilityDomain: strings.TrimSpace(g.adEntry.Text),
			DisplayName:        strings.TrimSpace(g.displayName.Text),
			Shape:              g.shapeSelect.Selected,
			OCPUs:              parseFloat(g.ocpus.Text, 4),
			MemoryGB:           parseFloat(g.memoryGB.Text, 24),
			ImageID:            strings.TrimSpace(g.imageID.Text),
			SubnetID:           strings.TrimSpace(g.subnetID.Text),
			AssignPublicIP:     g.assignPublicIP.Checked,
			SSHPublicKey:       strings.TrimSpace(g.sshKey.Text),
		},
		Scheduler: config.SchedulerConfig{
			IntervalSeconds:   parseInt(g.interval.Text, 60),
			JitterSeconds:     parseInt(g.jitter.Text, 15),
			MaxBackoffSeconds: parseInt(g.maxBackoff.Text, 600),
			MaxAttempts:       parseInt(g.maxAttempts.Text, 0),
		},
		Notify: config.NotifyConfig{
			WebhookURL:       strings.TrimSpace(g.webhookURL.Text),
			TelegramBotToken: strings.TrimSpace(g.tgBotToken.Text),
			TelegramChatID:   strings.TrimSpace(g.tgChatID.Text),
		},
	}
}

// ──────────────────────────────────────────────
// 抢机控制
// ──────────────────────────────────────────────

func (g *GUIApp) startGrabbing() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running {
		return
	}

	cfg := g.collectConfig()
	if err := config.Validate(cfg); err != nil {
		dialog.ShowError(fmt.Errorf("配置不完整: %w", err), g.win)
		return
	}

	g.attemptsLabel.SetText("尝试次数: 0")
	g.setStatus("🟡 运行中", true)

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

	instCfg := oci.InstanceConfig{
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

	g.sched = scheduler.New(scheduler.Config{
		IntervalSeconds:   cfg.Scheduler.IntervalSeconds,
		JitterSeconds:     cfg.Scheduler.JitterSeconds,
		MaxBackoffSeconds: cfg.Scheduler.MaxBackoffSeconds,
		MaxAttempts:       cfg.Scheduler.MaxAttempts,
		Client:            client,
		InstanceConfig:    instCfg,
		Notifier:          notifier,
	})
	g.running = true

	// 更新 attempts 显示（每秒轮询调度器计数器）
	go func() {
		for {
			g.mu.Lock()
			if !g.running || g.sched == nil {
				g.mu.Unlock()
				return
			}
			n := g.sched.Attempts()
			g.mu.Unlock()
			g.attemptsLabel.SetText(fmt.Sprintf("尝试次数: %d", n))
			time.Sleep(time.Second)
		}
	}()

	go func() {
		g.sched.Run()
		// 调度器退出（成功 or 主动停止）
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()
		g.setStatus("● 已停止", false)
	}()
}

func (g *GUIApp) stopGrabbing() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.running || g.sched == nil {
		return
	}
	g.sched.Stop()
	g.running = false
	g.setStatus("● 已停止", false)
}

// Stop 关闭窗口时调用
func (g *GUIApp) Stop() {
	g.stopGrabbing()
}

func (g *GUIApp) setStatus(text string, isRunning bool) {
	g.statusLabel.SetText(text)
	if isRunning {
		g.startBtn.Disable()
		g.stopBtn.Enable()
	} else {
		g.startBtn.Enable()
		g.stopBtn.Disable()
	}
}

// ──────────────────────────────────────────────
// 日志消费
// ──────────────────────────────────────────────

func (g *GUIApp) drainLogs() {
	const maxLen = 60000
	for msg := range g.logCh {
		cur := g.logEntry.Text
		if len(cur)+len(msg) > maxLen {
			// 保留后半段，丢弃过旧的内容
			cur = cur[len(cur)/2:]
			idx := strings.Index(cur, "\n")
			if idx >= 0 {
				cur = cur[idx+1:]
			}
		}
		g.logEntry.SetText(cur + msg)
		// 滚动到底部
		g.logEntry.CursorRow = strings.Count(g.logEntry.Text, "\n")
	}
}
