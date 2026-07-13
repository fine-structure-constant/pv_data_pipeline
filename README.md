# 钙钛矿文献爬取与数据提取

这是一个 Go + PostgreSQL 文献管线。Crossref 负责发现文献，DeepSeek 负责理解论文元数据并提取钙钛矿组分；程序支持模糊正向/反向关键词、按目标数量分页补足、去重入库及可选学术联网检索。现有 `serve` 路由和 JSON 数据格式保持兼容。

## 项目结构

```text
cmd/pvsk/                 CLI 入口：migrate、crawl、classify、download、serve
internal/crawler/         补足目标数量、模糊正/反向筛选、入库
internal/sources/         文献源接口与 Crossref 分页适配器
internal/classify/        规则分类、LLM 调用与组分持久化
internal/llm/             DeepSeek/OpenAI 兼容 API 与可选 web_search 工具
internal/models/          papers、materials、compositions 等数据库模型
internal/server/          原有网页和只读 JSON API
internal/download/        合规下载公开可访问资产
internal/data2/           既有 xlsx/csv 数据导入
prompts/                  LLM 提取提示词
config.example.yaml       配置模板
```

## 配置与初始化

需要 Go 1.22+ 和 PostgreSQL。复制配置后填写数据库 DSN：

```bash
cp config.example.yaml config.yaml
go mod download
go run ./cmd/pvsk --config config.yaml migrate
```

DeepSeek 使用 OpenAI 兼容接口：

```yaml
llm:
  provider: deepseek
  base_url: https://api.deepseek.com
  api_key: "YOUR_DEEPSEEK_API_KEY"
  model: deepseek-chat  # 也可以填写账户实际支持的其他模型
  timeout_seconds: 60
  enable_web_search: false
```

`config.yaml` 已被 `.gitignore` 忽略，可以保留本地测试 key，但不要把真实 key 写入 `config.example.yaml` 或提交到 Git。

### DeepSeek 联网工具

DeepSeek API 本身用于模型推理；项目通过 function calling 为它提供 `web_search`：

```text
论文标题/摘要 → DeepSeek
                  ├─ 信息充分：直接输出结构化 JSON
                  └─ 信息不足：调用 web_search
                                   ↓
                              Crossref 检索
                                   ↓
                    精简为最多 5 条 DOI/标题/摘要
                                   ↓
                         回传 DeepSeek 完成提取
```

工具最多进行 3 轮调用，单条摘要最多保留 1200 字符，避免原始 Crossref 响应挤占模型上下文。它只搜索学术元数据，不是通用网页浏览器，也不下载或绕过付费全文。设置 `enable_web_search: false` 可完全禁止模型调用该工具。

## 爬取和提取

`--limit` 表示最终接受的记录数，不是 Crossref 候选数。程序会分页补足，并过滤 Supporting Information DOI 和非论文记录。

```bash
# 目标：500 条包含 FAPbI3、排除任何提及 MAPbI3 的论文，并立即提取
go run ./cmd/pvsk --config config.yaml crawl \
  --query "FAPbI3 perovskite solar cell" \
  --include "FAPbI3" \
  --exclude "MAPbI3" \
  --limit 500 \
  --candidate-factor 20 \
  --extract
```

`--include` 与 `--exclude` 都支持逗号分隔；所有 include 必须匹配，任一 exclude 匹配即拒绝。匹配忽略大小写、空格和标点，例如 `FAPbI3` 可以匹配 `FA PbI3`。反向条件优先。

注意：`--exclude MAPbI3` 会排除“研究 FAPbI3 但摘要拿 MAPbI3 作对比”的文章。如果目标只是排除“纯 MAPbI3 体系”，不要传该反向词；爬取后使用 `MAPBI3_BASELINE` / `NOT_MA_PB_I3` 分类标签筛选更准确：

```bash
go run ./cmd/pvsk --config config.yaml crawl \
  --query "halide perovskite solar cell" \
  --include "perovskite,solar cell" \
  --limit 500 --candidate-factor 20 --extract
```

候选过少时提高 `--candidate-factor`。不带 `--extract` 时可稍后执行：

```bash
go run ./cmd/pvsk --config config.yaml classify --limit 500
```

有 API key 时，`detected_compositions` 会写入 `materials`、`compositions` 和 `paper_materials`；没有 key 时只执行规则分类，不会伪造结构化组分。

单独验证 DeepSeek 提取：

```bash
go run ./cmd/pvsk --config config.yaml classify --limit 1
```

项目已用真实 DeepSeek 请求验证普通分类和开启 `web_search` 后的工具声明；自动化测试还覆盖了完整的 tool-call 往返协议。模型只有在认为元数据不足时才会实际发起搜索，并非每篇论文都会调用。

## Serve 与读取 API

```bash
go run ./cmd/pvsk --config config.yaml serve --addr ":8080"
```

保留的接口：

```text
GET /healthz
GET /papers
GET /papers/{id}
GET /papers?tag=FA_PB_I3
GET /papers?query=wide-bandgap
GET /papers?download_status=open_access_downloaded
GET /papers?year=2025
GET /assets/{id}
GET /
```

其他命令：

```bash
go run ./cmd/pvsk --config config.yaml download --limit 100
go run ./cmd/pvsk --config config.yaml merge-data2 --file data.xlsx
```

下载器仅使用元数据公开的链接，不绕过出版商权限控制。

## 测试

```bash
env GOCACHE=/tmp/go-cache go test ./...
env GOCACHE=/tmp/go-cache go build -buildvcs=false ./...
```

早期测试产生过 SI 记录，可按 DOI 后缀检查，确认后再删除：

```sql
select id, doi, title from papers where doi ~* '\\.s[0-9]+$';
-- delete from papers where doi ~* '\\.s[0-9]+$';
```
