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
GITEA_BASE_URL=https://你的-gitea-地址
GITEA_TOKEN=你的-gitea-token
GITEA_WEBHOOK_SECRET=改成长随机-secret
```

生产推荐使用独立 PostgreSQL 或已经部署好的数据库。应用发布只更新 `api` 和 `web`，不重复部署数据库。

如果使用已有外部数据库，把 `DATABASE_URL` 的主机名从 `postgres` 改成真实数据库地址，并且不需要设置 `POSTGRES_PASSWORD`。

如果你暂时没有现成 PostgreSQL，可以先单独启动一次数据库服务：

```bash
docker compose --env-file .env.production -f docker-compose.db.yml up -d
```

这种方式会创建名为 `code_review_bot_postgres_data` 的 Docker volume。后续发布应用时不要执行 `down -v`，否则会删除数据。

如果要真实 AI review，还要填：

```env
OPENAI_API_KEY=你的-api-key
OPENAI_BASE_URL=https://api.openai.com/v1
REVIEW_MODEL=你的可用模型
```

如果 `OPENAI_API_KEY` 留空，系统会使用 mock reviewer，只适合验证 webhook、队列、Gitea status/comment 回写链路。

## 4. 启动应用服务

`docker-compose.prod.yml` 只包含应用服务，不包含数据库。这样每次发布不会影响数据服务。应用启动时会等待数据库最多 60 秒，避免数据库刚启动时连接失败。

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

    location /webhooks/gitea {
        proxy_pass http://127.0.0.1:8080/webhooks/gitea;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }

    location /healthz {
        proxy_pass http://127.0.0.1:8080/healthz;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }

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

当前 React Admin 还没有登录认证。

如果域名直接暴露到公网，建议至少先做一个限制：

### 方案 A：只暴露 webhook，不公开 Admin

Nginx 中只配置：

```nginx
location /webhooks/gitea { ... }
location /healthz { ... }
```

不要配置 `/` 和 `/api/` 给公网。

### 方案 B：用 Nginx basic auth 保护 Admin

给 `/` 和 `/api/` 加 basic auth。

示例：

```nginx
location / {
    auth_basic "Review Bot Admin";
    auth_basic_user_file /etc/nginx/.htpasswd;
    proxy_pass http://127.0.0.1:5173/;
}

location /api/ {
    auth_basic "Review Bot Admin";
    auth_basic_user_file /etc/nginx/.htpasswd;
    proxy_pass http://127.0.0.1:8080/api/;
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

如果你选择不公开 Admin，则跳过这一步。

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
和 .env.production 的 GITEA_WEBHOOK_SECRET 完全一致
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
