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

# GitHub Actions 无服务器运行配置

本项目可以不准备自己的服务器，直接用 GitHub Actions 定时运行：

1. GitHub Actions 每 30 分钟启动一次临时 Runner。
2. Runner 拉取本仓库代码并编译 CLI。
3. CLI 从 GitHub Secrets 读取 OCI 配置。
4. 程序调用 OCI API 尝试创建 Always Free ARM 实例。
5. 每轮最多运行约 28 分钟，结束后等待下一次定时任务。

注意：GitHub Actions 不是 24 小时常驻进程。它适合“定时轮询”，不是永久在线服务。

## 一、GitHub 仓库需要配置的 Secrets

进入 GitHub 仓库页面：

`Settings` -> `Secrets and variables` -> `Actions` -> `New repository secret`

逐个添加下面的 Secret。

| Secret 名称 | 是否必填 | 从哪里获取 | 说明 |
|---|---:|---|---|
| `OCI_TENANCY_OCID` | 必填 | OCI 控制台右上角头像 -> Tenancy 或 Administration -> Tenancy Details | 租户 OCID。根 Compartment 通常也使用这个值。 |
| `OCI_USER_OCID` | 必填 | OCI 控制台右上角头像 -> My profile -> User information | 当前 API 用户的 OCID。 |
| `OCI_FINGERPRINT` | 必填 | OCI 控制台右上角头像 -> My profile -> API keys | 上传 API 公钥后显示的 Fingerprint。 |
| `OCI_PRIVATE_KEY_CONTENT` | 必填 | 本地生成的 OCI API 私钥 `.pem` 文件 | 填整个私钥文件内容，包括 `-----BEGIN PRIVATE KEY-----` 和 `-----END PRIVATE KEY-----`。不要提交到 Git。 |
| `OCI_REGION` | 必填 | OCI 控制台顶部 Region 下拉框 | 例如 `ap-tokyo-1`、`ap-osaka-1`、`us-ashburn-1`。必须和你要创建实例的区域一致。 |
| `OCI_COMPARTMENT_ID` | 必填 | OCI 控制台 Identity -> Compartments，或 Tenancy Details | 要创建实例的 Compartment OCID。新账号通常可以先填 `OCI_TENANCY_OCID`。 |
| `OCI_SSH_PUBLIC_KEY` | 必填 | 你本地的 SSH 公钥文件，例如 `~/.ssh/id_rsa.pub` | 用于登录新创建的云主机。填公钥，不是私钥。 |
| `OCI_WEBHOOK_URL` | 可选 | 企业微信、钉钉等机器人 Webhook 地址 | 抢机成功或失败时推送通知。 |
| `OCI_TG_BOT_TOKEN` | 可选 | Telegram BotFather 创建机器人后获得 | 和 `OCI_TG_CHAT_ID` 一起使用。 |
| `OCI_TG_CHAT_ID` | 可选 | Telegram bot/getUpdates 或相关工具查询 | Telegram 通知接收 chat id。 |

## 二、OCI API Key 如何获取

### 1. 生成 API Key

如果你已经有 `oci_api_key.pem`，可以跳过生成步骤。

本项目本地生成过的 `.pem` 私钥只能放在本地或 GitHub Secrets，不能提交到仓库。

常见文件包括：

```text
oci_api_key.pem          # 私钥，放入 OCI_PRIVATE_KEY_CONTENT
oci_api_key_public.pem   # 公钥，上传到 OCI
```

如果你用 OCI 控制台生成 API Key，控制台会让你下载私钥并显示 Fingerprint。

### 2. 上传公钥到 OCI 用户

路径：

`OCI Console` -> 右上角头像 -> `My profile` -> `API keys` -> `Add API key`

选择上传公钥内容。上传成功后，页面会显示：

```text
Fingerprint
User OCID
Tenancy OCID
Region
```

这些值分别填入 GitHub Secrets。

### 3. 复制私钥到 GitHub Secret

打开本地私钥文件，把完整内容复制到：

```text
OCI_PRIVATE_KEY_CONTENT
```

格式示例：

```text
-----BEGIN PRIVATE KEY-----
...
-----END PRIVATE KEY-----
```

不要把 `.pem` 文件提交到 GitHub。当前 `.gitignore` 已经忽略 `*.pem`。

## 三、SSH 公钥如何获取

本项目创建实例时会把 SSH 公钥写入云主机 metadata。

Windows PowerShell 可以查看：

```powershell
Get-Content $env:USERPROFILE\.ssh\id_rsa.pub
```

如果没有 SSH key，可以生成：

```powershell
ssh-keygen -t rsa -b 4096 -C "your-email@example.com"
```

然后把 `.pub` 文件内容填入：

```text
OCI_SSH_PUBLIC_KEY
```

注意：这里填的是 `.pub` 公钥，不是私钥。

## 四、网络和镜像配置

当前 workflow 默认：

```yaml
OCI_IMAGE_ID: ""
OCI_SUBNET_ID: ""
```

表示程序会自动尝试查找：

1. 当前 Region 下可用的 Ubuntu ARM 镜像。
2. 当前 Compartment 下第一个可用 VCN 和 Subnet。

如果自动查找失败，可以手动配置：

| 环境变量 | 获取位置 | 说明 |
|---|---|---|
| `OCI_IMAGE_ID` | OCI Console -> Compute -> Custom Images 或镜像选择页 | 必须和 `OCI_REGION` 匹配，ARM 实例建议选 AArch64/ARM64 镜像。 |
| `OCI_SUBNET_ID` | OCI Console -> Networking -> Virtual cloud networks -> Subnets | 必须在目标 Compartment 和 Region 内。 |

如果你想手动固定它们，可以在 `.github/workflows/oci-grabber.yml` 里改成具体 OCID，也可以改成从 GitHub Secrets 读取。

## 五、实例规格配置

workflow 中默认配置：

```yaml
OCI_DISPLAY_NAME: "free-arm-github-01"
OCI_SHAPE: "VM.Standard.A1.Flex"
OCI_OCPUS: "4"
OCI_MEMORY_GB: "24"
OCI_ASSIGN_PUBLIC_IP: "true"
```

说明：

| 变量 | 说明 |
|---|---|
| `OCI_DISPLAY_NAME` | 新实例名称。 |
| `OCI_SHAPE` | 实例规格，Always Free ARM 常用 `VM.Standard.A1.Flex`。 |
| `OCI_OCPUS` | OCPU 数量。Always Free ARM 总额度通常是 4 OCPU。 |
| `OCI_MEMORY_GB` | 内存 GB。Always Free ARM 总额度通常是 24GB。 |
| `OCI_ASSIGN_PUBLIC_IP` | 是否分配公网 IP，通常填 `true`。 |

如果你账号里已经有其他 A1 实例，占用了免费额度，需要降低 `OCI_OCPUS` 和 `OCI_MEMORY_GB`。

## 六、调度参数

workflow 中默认配置：

```yaml
OCI_INTERVAL_SECONDS: "60"
OCI_JITTER_SECONDS: "10"
OCI_MAX_BACKOFF_SECONDS: "120"
OCI_MAX_ATTEMPTS: "25"
```

说明：

| 变量 | 说明 |
|---|---|
| `OCI_INTERVAL_SECONDS` | 每次尝试之间的基础间隔。 |
| `OCI_JITTER_SECONDS` | 随机抖动秒数，避免请求过于固定。 |
| `OCI_MAX_BACKOFF_SECONDS` | 连续失败后的最大退避等待。 |
| `OCI_MAX_ATTEMPTS` | 每轮 GitHub Actions 最多尝试次数。 |

配合 GitHub Actions 的 `timeout-minutes: 28` 和 `cron: "*/30 * * * *"`，每轮最多跑 28 分钟，每 30 分钟启动一次。

## 七、GitHub Actions 文件应使用的配置

建议 `.github/workflows/oci-grabber.yml` 保持类似下面的结构：

```yaml
name: OCI Auto Grabber

on:
  schedule:
    - cron: "*/30 * * * *"
  workflow_dispatch:

jobs:
  grab:
    runs-on: ubuntu-latest
    timeout-minutes: 28

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.21"
          cache: true

      - name: Build CLI
        run: go build -o oci-grabber-cli .

      - name: Start grabbing
        env:
          OCI_TENANCY_OCID: ${{ secrets.OCI_TENANCY_OCID }}
          OCI_USER_OCID: ${{ secrets.OCI_USER_OCID }}
          OCI_FINGERPRINT: ${{ secrets.OCI_FINGERPRINT }}
          OCI_PRIVATE_KEY_CONTENT: ${{ secrets.OCI_PRIVATE_KEY_CONTENT }}
          OCI_REGION: ${{ secrets.OCI_REGION }}
          OCI_COMPARTMENT_ID: ${{ secrets.OCI_COMPARTMENT_ID }}
          OCI_SSH_PUBLIC_KEY: ${{ secrets.OCI_SSH_PUBLIC_KEY }}

          OCI_DISPLAY_NAME: "free-arm-github-01"
          OCI_SHAPE: "VM.Standard.A1.Flex"
          OCI_OCPUS: "4"
          OCI_MEMORY_GB: "24"
          OCI_ASSIGN_PUBLIC_IP: "true"

          OCI_IMAGE_ID: ""
          OCI_SUBNET_ID: ""

          OCI_INTERVAL_SECONDS: "60"
          OCI_JITTER_SECONDS: "10"
          OCI_MAX_BACKOFF_SECONDS: "120"
          OCI_MAX_ATTEMPTS: "25"

          OCI_WEBHOOK_URL: ${{ secrets.OCI_WEBHOOK_URL }}
          OCI_TG_BOT_TOKEN: ${{ secrets.OCI_TG_BOT_TOKEN }}
          OCI_TG_CHAT_ID: ${{ secrets.OCI_TG_CHAT_ID }}
        run: ./oci-grabber-cli
```

注意：当前仓库里的 workflow 如果出现中文乱码，并且把 `- cron:`、`env:` 或 `OCI_PRIVATE_KEY_CONTENT:` 放进了注释行，需要修复后再运行。

## 八、如何手动测试

配置好 Secrets 后：

1. 打开 GitHub 仓库。
2. 进入 `Actions`。
3. 选择 `OCI Auto Grabber`。
4. 点击 `Run workflow`。
5. 打开运行日志，确认以下步骤成功：
   - Checkout repository
   - Set up Go
   - Build CLI
   - Start grabbing

如果日志里出现 `auth.tenancy_ocid 不能为空` 或类似配置错误，说明 Secrets 没有正确注入，优先检查 workflow 的 `env:` 是否有效。

## 九、不要提交到 GitHub 的内容

下面这些只能留在本地或 GitHub Secrets：

```text
*.pem
config.toml
*.exe
.tools/
.opencode/
```

尤其是：

```text
oci_api_key.pem
```

它是 OCI API 私钥，泄露后别人可能操作你的 OCI 账号。

---
