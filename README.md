# 甲骨文OCI自动抢机脚本 (OCI Instance Auto-Grabber)

> 基于 Oracle Cloud Infrastructure (OCI) 官方 REST API，全自动轮询抢占免费 ARM 实例。

---

## 一、背景与目标

Oracle Cloud 免费套餐（Always Free）提供 Ampere A1 ARM 架构实例，资源紧俏，控制台手动创建几乎必定返回：
```
Out of host capacity. Please wait for capacity.
```

本工具通过 **OCI REST API 签名认证 + 高频率轮询重试**，在后台全自动抢机，一旦出现可用容量立即下单，无需人工盯屏。

---

## 二、技术路线

```
┌─────────────────────────────────────────────────────────┐
│                   OCI Auto Grabber                      │
│                                                         │
│  ┌─────────────┐   签名请求    ┌──────────────────────┐ │
│  │  Config     │ ──────────►  │  OCI REST API        │ │
│  │  (TOML)     │              │  LaunchInstance      │ │
│  └─────────────┘              └──────────┬───────────┘ │
│                                          │              │
│  ┌─────────────┐   成功/失败             ▼              │
│  │  Scheduler  │ ◄──────────  ┌──────────────────────┐ │
│  │  (Ticker)   │              │  Response Parser      │ │
│  └──────┬──────┘              └──────────────────────┘ │
│         │                                               │
│         ▼                                               │
│  ┌─────────────┐                                        │
│  │  Notifier   │  (Desktop / Webhook / Email)           │
│  └─────────────┘                                        │
└─────────────────────────────────────────────────────────┘
```

### 核心流程

1. 读取 `config.toml` 配置（OCID、私钥、实例规格等）
2. 构造 `LaunchInstance` POST 请求体（JSON）
3. 使用 OCI **HTTP Signature v1** 对请求签名
4. 发送至 OCI API，解析响应
5. 若失败（容量不足 / 限速），等待间隔后重试
6. 若成功，记录实例信息并发送通知

---

## 三、实现方向

### 3.1 认证方式：OCI HTTP Signature

OCI API 不使用 Bearer Token，采用 **RSA + SHA-256 HTTP Signature**：

| 字段           | 说明                              |
|--------------|----------------------------------|
| keyId        | `tenancy/user/fingerprint` 三段拼接 |
| algorithm    | `rsa-sha256`                     |
| headers      | `date host (request-target) content-type content-length x-content-sha256` |
| signature    | Base64(RSA-SHA256(signing_string, private_key)) |

### 3.2 目标 API

| API | Endpoint |
|-----|----------|
| LaunchInstance | `POST /20160918/instances` |
| ListInstances  | `GET  /20160918/instances` |
| GetInstance    | `GET  /20160918/instances/{instanceId}` |

Base URL: `https://iaas.{region}.oraclecloud.com`

### 3.3 抢机策略

| 策略 | 说明 |
|------|------|
| 主动轮询 | 每 N 秒发起一次 LaunchInstance，默认 60s |
| 随机抖动 | 基础间隔 ± 随机抖动，避免规律性封禁 |
| 指数退避 | 连续失败后指数级增加等待（上限可配）|
| 多可用域 | 并发尝试同 Region 下多个 AD（Availability Domain）|
| 失败分类 | 区分容量不足、限流、鉴权失败，分别处理 |

### 3.4 通知方式

- **控制台** stdout 彩色日志（默认）
- **Webhook** 企业微信 / 钉钉 / Telegram Bot
- **系统通知** Windows toast（可选）

---

## 四、项目结构

```
JGW/
├── README.md               ← 本文档
├── config.toml             ← 用户配置（不提交 git）
├── config.example.toml     ← 配置模板
├── go.mod
├── go.sum
├── main.go                 ← 入口：解析配置 + 启动调度器
├── internal/
│   ├── auth/
│   │   └── signer.go       ← OCI HTTP Signature 签名器
│   ├── oci/
│   │   ├── client.go       ← HTTP 客户端封装
│   │   ├── instance.go     ← LaunchInstance / ListInstances
│   │   └── models.go       ← 请求 / 响应结构体
│   ├── scheduler/
│   │   └── scheduler.go    ← 轮询调度 + 退避逻辑
│   └── notify/
│       └── notify.go       ← 通知发送（Webhook / 控制台）
└── logs/
    └── .gitkeep
```

---

## 五、配置说明（config.toml）

```toml
[auth]
tenancy_ocid   = "ocid1.tenancy.oc1..aaaa..."
user_ocid      = "ocid1.user.oc1..aaaa..."
fingerprint    = "xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx"
private_key_path = "./oci_private_key.pem"
region         = "ap-tokyo-1"          # 你的区域

[instance]
compartment_id = "ocid1.compartment.oc1..aaaa..."  # 通常等于 tenancy_ocid
availability_domain = ""               # 留空则自动轮询所有可用域 (AD)
display_name   = "free-arm-01"
shape          = "VM.Standard.A1.Flex"
ocpus          = 4
memory_gb      = 24
image_id       = ""                    # 💡 留空：自动搜索匹配的 Ubuntu 22.04 Aarch64 镜像
subnet_id      = ""                    # 💡 留空：自动选用你的第一个可用 VCN 和子网
assign_public_ip = true
ssh_public_key = "ssh-rsa AAAA..."

[scheduler]
interval_seconds   = 60        # 基础轮询间隔（秒）
jitter_seconds     = 15        # 随机抖动范围
max_backoff_seconds = 600      # 最大退避上限（10分钟）
max_attempts       = 0         # 0 = 无限重试

[notify]
webhook_url    = ""            # 企微/钉钉/Telegram Webhook，留空则只打印日志
```

---

## 六、使用方法

### 方案 A：本地或本地 VPS 运行
```bash
# 1. 进入目录
cd d:\AI\JGW

# 2. 复制并填写配置（也可以通过 GUI 填写和保存）
copy config.example.toml config.toml

# 3. 运行（支持 CLI 运行，或双击运行 GUI）
go run .
```

---

## ⚡ 方案 B：GitHub Actions 免服务器抢机（推荐）

本程序内置了 GitHub Actions 工作流。**将本仓库 push 到你自己的 GitHub 账号上**，并在仓库设置里填写 Secrets 凭证，GitHub 服务器就会以定时任务的形式（每 30 分钟一轮）全自动在后台轮询抢机，无需你本地开机或购买 VPS。

### 1. 推送到 GitHub
```bash
cd d:\AI\JGW
git init
git remote add origin https://github.com/你的用户名/oci-grabber.git
git add .
git commit -m "init: OCI auto grabber"
git push -u origin main
```

### 2. 配置 GitHub Secrets
进入你的 GitHub 仓库，依次点击 `Settings` → `Secrets and variables` → `Actions` → `New repository secret`，添加以下机密：

| Secret Name | 示例值 / 说明 |
|-------------|--------------|
| `OCI_TENANCY_OCID` | `ocid1.tenancy.oc1..aaaa...` |
| `OCI_USER_OCID` | `ocid1.user.oc1..aaaa...` |
| `OCI_FINGERPRINT` | `xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx:xx` |
| `OCI_PRIVATE_KEY_CONTENT` | 📝 **你的 `.pem` 密钥的完整文本内容**（包含 `-----BEGIN RSA PRIVATE KEY-----` 等所有行） |
| `OCI_REGION` | `ap-tokyo-1` 或你开户的区域 |
| `OCI_COMPARTMENT_ID` | `ocid1.tenancy.oc1..aaaa...`（通常和 Tenancy OCID 相同） |
| `OCI_SSH_PUBLIC_KEY` | `ssh-rsa AAAA...`（你用于登录新主机的 SSH 公钥） |
| `OCI_WEBHOOK_URL` | *(可选)* 企微/钉钉 Webhook 通知地址 |

### 3. 启用工作流 (Workflow)
在 GitHub 仓库主页，点击顶部的 `Actions` 选项卡，在左侧选择 `OCI Auto Grabber (甲骨文全自动抢机)`，点击 **`Enable workflow`** 启用它。之后你可以随时点击 `Run workflow` 手动开始抢机测试，或者等待 GitHub 自动定时触发。

---

## 七、关键注意事项

> [!WARNING]
> 请妥善保管 `config.toml` 和 `OCI_PRIVATE_KEY_CONTENT`，切勿将包含私钥或真实 OCID 的配置文件提交到 GitHub 公开仓库中。使用 GitHub Actions 时，**必须使用 Secrets 机制**。

> [!NOTE]
> - 甲骨文免费账户最多允许创建 4个 OCPU + 24GB RAM 的 ARM 实例。
> - 本程序内置了 **动态元数据匹配**。若 `image_id` 或 `subnet_id` 留空，程序在启动时会自动定位最新 Ubuntu 镜像和默认网络，无需再从后台到处抓取 ID。

---

## 八、优化与实现进度

- [x] 自动获取可用镜像 ID（ListImages - **已完成** 🚀）
- [x] 自动获取子网和 VCN（ListVcns / ListSubnets - **已完成** 🚀）
- [x] 自动获取 Availability Domain 列表（**已完成**）
- [x] 完美支持 GitHub Actions 定时任务免服务器抢机（**已完成** ⚡）
- [x] 跨平台 GUI 图形界面版（**已完成** 💻）
- [ ] 支持多账号并发抢机
- [ ] 支持双向 Telegram Bot 交互指令控制

