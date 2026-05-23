# VPS 线上部署说明

适用场景：

- 一台 Linux VPS
- 已有域名和 HTTPS 方案
- 使用 Docker Compose 部署 bot
- 由 Nginx / 宝塔 / 现有网关反向代理到容器

下面假设你的线上域名是：

```text
https://reviewbot.example.com
```

请替换成你自己的域名。

## 1. 服务器准备

服务器需要安装：

- Docker
- Docker Compose plugin
- Git

检查：

```bash
docker --version
docker compose version
git --version
```

## 2. 上传代码

方式 A：服务器直接 clone：

```bash
git clone <你的仓库地址> /opt/code-review-bot
cd /opt/code-review-bot
```

方式 B：本机打包上传：

```bash
tar --exclude web/node_modules --exclude web/dist --exclude .idea -czf code-review-bot.tar.gz .
scp code-review-bot.tar.gz root@你的服务器:/opt/
```

服务器上解压：

```bash
mkdir -p /opt/code-review-bot
tar -xzf /opt/code-review-bot.tar.gz -C /opt/code-review-bot
cd /opt/code-review-bot
```

## 3. 创建生产环境变量

```bash
cp .env.production.example .env.production
nano .env.production
```

必须修改：

```env
DATABASE_URL=postgres://reviewbot:数据库密码@postgres:5432/reviewbot?sslmode=disable
POSTGRES_PASSWORD=数据库密码
SESSION_SECRET=改成长随机-session-secret
```

Gitea、AI、review 策略等运行期配置不再写入 `.env.production`，首次打开 Admin 时通过安装向导写入数据库。

生产推荐使用独立 PostgreSQL 或已经部署好的数据库。应用发布只更新 `api` 和 `web`，不重复部署数据库。

如果使用已有外部数据库，把 `DATABASE_URL` 的主机名从 `postgres` 改成真实数据库地址，并且不需要设置 `POSTGRES_PASSWORD`。

如果你暂时没有现成 PostgreSQL，可以先单独启动一次数据库服务：

```bash
docker compose --env-file .env.production -f docker-compose.db.yml up -d
```

这种方式会创建名为 `code_review_bot_postgres_data` 的 Docker volume。后续发布应用时不要执行 `down -v`，否则会删除数据。

如果要真实 AI review，在安装向导或 Admin 系统配置里填写 OpenAI API Key、Base URL 和 Review Model。如果 OpenAI API Key 留空，系统会使用 mock reviewer，只适合验证 webhook、队列、Gitea status/comment 回写链路。

## 4. 启动应用服务

`docker-compose.prod.yml` 只包含应用服务，不包含数据库。这样每次发布不会影响数据服务。应用启动时会等待数据库最多 60 秒，随后使用 `pressly/goose` 按 `internal/db/migrations/*.sql` 自动执行未应用的 migration，并记录到 `goose_db_version` 表。

```bash
docker compose --env-file .env.production -f docker-compose.prod.yml up -d --build
```

查看状态：

```bash
docker compose --env-file .env.production -f docker-compose.prod.yml ps
```

查看日志：

```bash
docker compose --env-file .env.production -f docker-compose.prod.yml logs -f api
```

## 5. Nginx 反向代理

如果你已有 HTTPS 证书和 Nginx，可以加一个 server 配置。

示例：

```nginx
server {
    listen 443 ssl http2;
    server_name reviewbot.example.com;

    ssl_certificate /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;

    client_max_body_size 20m;

    location / {
        proxy_pass http://127.0.0.1:5173/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

重载 Nginx：

```bash
nginx -t
systemctl reload nginx
```

## 6. 重要安全提醒

React Admin 已有内置管理员登录，首次访问会创建第一个管理员。生产环境必须设置 `SESSION_SECRET`，否则服务会拒绝启动；也可以按需叠加 Nginx basic auth 或 IP 限制。

如果域名直接暴露到公网，可以选择额外限制：

### 方案 A：只暴露 webhook，不公开 Admin

如果暂时不想公开 Admin，可以只给 webhook 单独配置一个域名或 location，并把其他路径返回 404。注意当前生产 compose 只把 `web` 映射到宿主机，`api` 不直接映射端口；这种模式需要额外把 API 映射到本地端口，或让宿主机 Nginx 加入 Docker 网络。

### 方案 B：用 Nginx basic auth 保护 Admin

给 `/` 加 basic auth。`web` 容器会在内部把 `/api/` 转发到 `api:8080`。

示例：

```nginx
location / {
    auth_basic "Review Bot Admin";
    auth_basic_user_file /etc/nginx/.htpasswd;
    proxy_pass http://127.0.0.1:5173/;
}
```

生成密码文件：

```bash
apt-get update
apt-get install -y apache2-utils
htpasswd -c /etc/nginx/.htpasswd admin
nginx -t
systemctl reload nginx
```

## 7. 验证部署

健康检查：

```bash
curl https://reviewbot.example.com/healthz
```

期望：

```json
{"status":"ok"}
```

打开 Admin：

```text
https://reviewbot.example.com
```

首次打开会进入安装向导，创建管理员账号，并填写 Gitea、AI 和 review 策略配置。如果你选择不公开 Admin，则跳过这一步。

## 8. 配置 Gitea Webhook

在 Gitea 测试仓库里新增 webhook。

Webhook URL：

```text
https://reviewbot.example.com/webhooks/gitea
```

Content Type：

```text
application/json
```

Secret：

```text
和 Admin 系统配置里的 Webhook Secret 完全一致
```

Events：

```text
Pull Request
```

建议先只在测试仓库配置仓库级 webhook，不建议一开始配用户级或组织级 webhook。

## 9. 创建测试 PR

在测试仓库创建一个 PR 后，查看日志：

```bash
docker compose --env-file .env.production -f docker-compose.prod.yml logs -f api
```

成功时会看到类似：

```text
queued review job
processing review job
completed review job
```

PR 页面应该出现：

- commit status：`code-review-bot/review`
- 一条 summary comment

## 10. 更新部署

拉取新代码后：

```bash
cd /opt/code-review-bot
docker compose --env-file .env.production -f docker-compose.prod.yml up -d --build
```

## 11. 停止应用服务

```bash
docker compose --env-file .env.production -f docker-compose.prod.yml down
```

这只停止 `api` 和 `web`，不会停止或删除数据库。

如果你使用 `docker-compose.db.yml` 自建了 PostgreSQL，数据库要单独管理：

```bash
docker compose --env-file .env.production -f docker-compose.db.yml stop
```

不要在生产环境随意执行：

```bash
docker compose --env-file .env.production -f docker-compose.db.yml down -v
```

`-v` 会删除 PostgreSQL 数据卷。
