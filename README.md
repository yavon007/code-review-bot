# Gitea PR Code Review Bot

一个基于 **Go + React** 的 Gitea PR 代码审查 Bot。

当前版本是 MVP：

- 接收 Gitea PR webhook
- 校验 webhook HMAC 签名
- 按 PR head commit 去重
- 将 review job 存入 PostgreSQL
- Worker 拉取 PR changed files 和 diff
- 调用 OpenAI-compatible Responses API 生成 summary review
- 回写 Gitea commit status
- 回写 PR summary comment
- React Admin 查看 job 状态

当前版本暂不做 inline comments、REQUEST_CHANGES、仓库配置文件和合并阻断。

## 目录结构

```text
.
├── main.go                    # Go 后端入口
├── internal/                  # 后端内部包
│   ├── config                 # 环境变量配置
│   ├── db                     # PostgreSQL 连接和 schema
│   ├── gitea                  # Gitea API client
│   ├── jobs                   # job store
│   ├── review                 # mock / OpenAI reviewer
│   ├── server                 # HTTP server
│   ├── webhook                # Gitea webhook 验签和解析
│   └── worker                 # 异步 review worker
├── web/                       # React Admin
├── docker-compose.yml
├── Dockerfile
├── .env.example
└── DESIGN.md
```

## 1. 准备 Gitea Bot Token

建议新建一个专用 Gitea bot 用户，然后在 Gitea Web UI 里创建 access token。

Token 至少需要能：

- 读取仓库
- 读取 Pull Request
- 读取 PR changed files / diff
- 创建 PR comment
- 创建 commit status

Gitea 里通常在：

```text
Settings -> Applications -> Generate New Token
```

创建后保存 token，后面填到 `.env`：

```env
GITEA_TOKEN=你的_gitea_token
```

## 2. 准备 Webhook Secret

自己生成一个随机字符串，例如：

```text
reviewbot-dev-secret-please-change
```

这个值要同时填到：

1. `.env` 的 `GITEA_WEBHOOK_SECRET`
2. Gitea 仓库 webhook 的 Secret

## 3. 准备模型配置

如果你只是先验证链路，可以先不填 `OPENAI_API_KEY`。

不填时：

- Bot 会正常接收 webhook
- Worker 会尝试拉 PR files / diff
- Review 阶段使用 mock reviewer
- 不会调用外部模型

如果要验证真实 AI review，需要填写：

```env
OPENAI_API_KEY=你的_openai_key
OPENAI_BASE_URL=https://api.openai.com/v1
REVIEW_MODEL=你的可用模型
```

注意：`REVIEW_MODEL` 必须是你账号实际可用的模型名。

## 4. 创建 `.env`

在项目根目录复制示例配置：

```bash
cp .env.example .env
```

然后编辑 `.env`。

最小可用配置示例：

```env
PORT=8080
DATABASE_URL=postgres://reviewbot:reviewbot@localhost:5432/reviewbot?sslmode=disable

GITEA_BASE_URL=https://gitea.example.com
GITEA_TOKEN=填写你的_gitea_token
GITEA_WEBHOOK_SECRET=填写你的_webhook_secret
BOT_NAME=gpt-review-bot

OPENAI_API_KEY=
OPENAI_BASE_URL=https://api.openai.com/v1
REVIEW_MODEL=gpt-4.1
REVIEW_MAX_DIFF_BYTES=120000

WORKER_POLL_INTERVAL=5s
WORKER_CONCURRENCY=1
```

如果使用 Docker Compose，`DATABASE_URL` 在 compose 里会自动设置为容器内地址：

```env
postgres://reviewbot:reviewbot@postgres:5432/reviewbot?sslmode=disable
```

所以本地 `.env` 里即使写了 localhost，`docker-compose.yml` 里的配置也会覆盖它。

## 5. 启动服务

在项目根目录运行：

```bash
docker compose --env-file .env up --build
```

启动后服务地址：

```text
Go API:      http://localhost:8080
Healthz:     http://localhost:8080/healthz
React Admin: http://localhost:5173
PostgreSQL:  localhost:5432
```

验证后端健康检查：

```bash
curl http://localhost:8080/healthz
```

期望返回：

```json
{"status":"ok"}
```

## 6. 配置 Gitea Webhook

进入测试仓库的 webhook 配置页面，新增 webhook。

Webhook URL：

```text
http://你的机器地址:8080/webhooks/gitea
```

如果 Gitea 和 bot 在同一台机器上，可以根据网络环境使用：

```text
http://host.docker.internal:8080/webhooks/gitea
```

或你的局域网 IP：

```text
http://192.168.x.x:8080/webhooks/gitea
```

Content Type：

```text
application/json
```

Secret：

```text
和 .env 里的 GITEA_WEBHOOK_SECRET 完全一致
```

事件选择：

```text
Pull Request
```

需要覆盖的场景：

- PR opened
- PR synchronized / push 新 commit
- PR reopened / edited，可选

## 7. 验证流程

1. 启动服务：

```bash
docker compose --env-file .env up --build
```

2. 打开 React Admin：

```text
http://localhost:5173
```

3. 在 Gitea 测试仓库创建一个 PR。

4. 观察后端日志，应该看到类似：

```text
queued review job
processing review job
completed review job
```

5. React Admin 应该出现一条 job，状态最终变成：

```text
succeeded
```

6. Gitea PR 页面应该出现：

- commit status：`code-review-bot/review`
- 一条 bot summary comment

## 8. 常见问题

### Webhook 返回 401 invalid signature

通常是 secret 不一致。

检查：

- `.env` 里的 `GITEA_WEBHOOK_SECRET`
- Gitea webhook 页面里的 Secret
- 修改 `.env` 后是否重启了容器

重启：

```bash
docker compose --env-file .env up --build
```

### React Admin 为空

可能还没有 webhook 入队。

检查后端健康：

```bash
curl http://localhost:8080/healthz
```

检查容器日志：

```bash
docker compose logs -f api
```

### Job 卡在 queued

通常是 worker 没启动。

检查 `.env` 或 compose 环境变量：

```env
GITEA_BASE_URL=必须填写
GITEA_TOKEN=必须填写
```

如果这两个为空，worker 会被禁用。

### Job 变成 errored

看 React Admin 的错误字段，或者查看日志：

```bash
docker compose logs -f api
```

常见原因：

- Gitea token 权限不足
- `GITEA_BASE_URL` 不正确
- Bot 无法访问 Gitea
- Gitea API 路径和当前 Gitea 版本不兼容
- 模型 API key 或模型名不正确

### 没有 OPENAI_API_KEY 会怎样？

可以正常启动。

Worker 会使用 mock reviewer，不会调用模型。适合先验证 webhook、队列、Gitea status/comment 回写链路。

### Gitea 是远程服务器，本机 bot 收不到 webhook

远程 Gitea 必须能访问你的 bot 地址。

如果 bot 在本机，可以用：

- 公网服务器
- 内网穿透
- VPN
- frp
- cloudflared
- ngrok

把本机 `8080` 暴露给 Gitea。

## 9. 本地开发命令

后端测试：

```bash
go test ./...
```

前端安装依赖：

```bash
npm install --prefix web
```

前端构建：

```bash
npm run build --prefix web
```

校验 Docker Compose：

```bash
docker compose config
```

## 10. 当前限制

当前版本是 MVP，还有这些能力未完成：

- inline comments
- diff 行号映射
- finding 去重
- summary comment 更新而不是每次新增
- stale head sha 二次检查
- 仓库白名单
- secret 检测和脱敏
- `.gitea-review-bot.yml`
- PR comment commands
- React Admin 认证
- 成本统计
- 质量评估集

建议先用测试仓库验证 MVP 闭环，确认稳定后再进入 inline review 和质量治理阶段。
