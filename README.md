# Perovskite Solar Cell Literature Pipeline

这个 Go 项目用于构建钙钛矿太阳能电池文献目录库，第一阶段聚焦“文献发现 + 元数据入库 + 合规开放获取下载 + LLM/规则分类 + 查询 API”。现有 `data_crawl_pdf_llm_code/` Python PDF/SI/LLM 抽取代码已保留不动，可作为后续全文解析和性能数据抽取链路。

重点优先收集非纯 `MAPbI3` 中心体系，例如 FA-rich、Cs-containing、I/Br mixed、Sn-based、wide-bandgap、mixed-cation、mixed-halide perovskites。出现 `MAPbI3` 不会被简单排除，只有纯 baseline 才会标记为 `MAPBI3_BASELINE`。

## Database Design

数据库按层分离：

- literature 层：`papers`、`paper_assets`、`material_classes`、`paper_material_classes`、`llm_classifications`、`crawl_jobs`、`crawl_logs`
- materials 层预留：`materials`、`compositions`、`structures`、`devices`、`measurements`

`paper_material_classes` 是多标签关联表，一篇论文可以同时属于 `FA_RICH`、`CS_CONTAINING`、`MIXED_HALIDE`、`WIDE_BANDGAP` 等类别。文件存储不按材料类别分目录，而是按 paper UUID：

```text
/home/rocky/HDDdata/perovskite_papers/{paper_uuid}/
  metadata.json
  paper.pdf
  fulltext.html
  supplementary/
```

`paper_assets` 记录真实路径、source URL、sha256、MIME type、license、access type 和下载错误。

## PostgreSQL Setup

示例：

```bash
createuser pvsk_app
createdb pvsk_db
psql -d pvsk_db -c "ALTER DATABASE pvsk_db OWNER TO pvsk_app;"
psql -d pvsk_db -c "ALTER USER pvsk_app WITH PASSWORD 'password';"
```

配置文件：

```bash
cp config.example.yaml config.yaml
```

运行时默认读取当前目录下的 `config.yaml`，也可以在子命令前用 `--config` 指定其他路径：

```bash
go run ./cmd/pvsk --config /path/to/config.yaml migrate
```

`config.yaml` 使用 YAML 配置表，不再读取 `.env` 或环境变量。常用字段包括数据库 DSN、文件存储目录、LLM 兼容接口、HTTP timeout、爬取限速和下载大小上限，完整示例见 `config.example.yaml`。

## Commands

安装依赖后迁移：

```bash
go mod download
go run ./cmd/pvsk --config config.yaml migrate
```

爬取公开元数据：

```bash
go run ./cmd/pvsk --config config.yaml crawl --query "FA Pb I3 perovskite solar cell" --limit 20
go run ./cmd/pvsk --config config.yaml crawl --query "Cs Pb I2 Br wide bandgap perovskite solar cell" --limit 20
go run ./cmd/pvsk --config config.yaml crawl --query "FA0.85 MA0.15 Pb I2.55 Br0.45 perovskite solar cell" --limit 20
go run ./cmd/pvsk --config config.yaml crawl --query "FA Sn I3 perovskite solar cell" --limit 20
```

分类：

```bash
go run ./cmd/pvsk --config config.yaml classify --limit 20
```

如需启用 LLM，在 `config.yaml` 中填写：

```yaml
llm:
  provider: openai_or_compatible
  base_url: https://api.openai.com/v1
  api_key: ...
  model: gpt-5-mini
  timeout_seconds: 60
```

没有 API key 时不会崩溃，会使用 rule-based fallback 并记录跳过原因。

下载开放获取资产：

```bash
go run ./cmd/pvsk --config config.yaml download --limit 20
```

合并 `data2` 格式的数据：

```bash
go run ./cmd/pvsk --config config.yaml merge-data2 --file ../data2_progress.xlsx
```

`merge-data2` 支持 `.xlsx` 和 `.csv`。导入时按 DOI 匹配或创建 `papers`，按 `perovskite_composition` 合并 `materials`/`compositions`，把 `solar_cell_structure` 和 `additive_abbreviation` 合并为 `devices.stack`，并把 `pce_after` 写入 `measurements.pce`；`pce_before`、PCE delta、CAS、PubChem、SMILES、分子式和原始行会保存在 JSON metadata 中。重复运行会更新同一 DOI + 材料 + 结构/添加剂组合的记录。

启动查询服务：

```bash
go run ./cmd/pvsk --config config.yaml serve --addr ":8080"
```

Web 监听地址也可以写在配置文件中；命令行 `--addr` 会覆盖配置值：

```yaml
web:
  addr: ":8080"
```

API：

```text
GET /healthz
GET /papers
GET /papers/{id}
GET /papers?tag=FA_PB_I3
GET /papers?query=wide-bandgap
GET /papers?download_status=open_access_downloaded
GET /assets/{id}
GET /
GET /api/tables
GET /api/tables/{table}
GET /download/tables/{table}.json
GET /download/imported-data2.json
```

打开 `/` 会进入 Web UI：左侧列出所有业务表，右侧以表格方式展示数据，支持关键词搜索、行数限制和当前表 JSON 下载。`/download/imported-data2.json` 会把 `merge-data2` 导入的 `source_api=data2` 记录打包为 JSON，包含 paper、materials、devices 和 measurements，可在 UI 中一键下载。

## Prompt

分类 prompt 位于 `prompts/classify_paper.md`，要求模型只输出 JSON。原始响应和解析后的 JSON 会写入 `llm_classifications`，解析失败不会中断 pipeline。

## 查看数据库

```bash
psql "postgres://pvsk_app:password@127.0.0.1:5432/pvsk_db?sslmode=disable"
\dt
select doi,title,year,download_status from papers order by created_at desc limit 10;
select p.doi, mc.code, pmc.confidence, pmc.assigned_by
from paper_material_classes pmc
join papers p on p.id = pmc.paper_id
join material_classes mc on mc.id = pmc.material_class_id
order by pmc.created_at desc
limit 20;
```

## 合规下载策略

- 当前 source adapter 默认使用 Crossref 元数据 API。
- 只下载元数据中暴露的 open-access link，不绕过出版社付费墙。
- 失败写入 `paper_assets.error_message` 和 `papers.download_status`，不会中断整批任务。
- HTTP 请求带 User-Agent、timeout、rate limit。
- 不硬编码账号、API key、cookie 或 token。

## Testing

```bash
go test ./...
go build ./...
```

已有单元测试覆盖 DOI normalize、规则 tag mapping、LLM JSON parsing。

如果当前工作目录不是完整 git 仓库，或者 Go cache 所在的 home 目录不可写，可用：

```bash
env GOCACHE=/tmp/go-cache go test ./...
env GOCACHE=/tmp/go-cache go build -buildvcs=false ./...
```

## Current Limits / TODO

- 已实现 Crossref source；OpenAlex、Semantic Scholar、Unpaywall、本地 JSON/CSV 导入已有接口位置，尚未实现。
- Crossref 的 PDF link 覆盖率有限；建议后续加入 Unpaywall 获取 OA location。
- 目前下载主 PDF/HTML/XML 资产；supplementary routing 可复用现有 Python `data_crawl_pdf_llm_code/scripts/si_download_lib.py` 的设计迁移。
- materials 层已经支持从 `data2` 表合并材料、器件和 PCE；尚未从全文自动抽取 Voc、Jsc、FF、bandgap、制备条件。

## Future Extension

后续建议流程：

1. 用现有 Python PDF/SI 解析链路或 GROBID/MinerU 把 `paper_assets.local_path` 转成正文文本。
2. 新增 extraction worker，把结构、器件栈、制备条件和性能指标写入 `materials`、`compositions`、`devices`、`measurements`。
3. 在 `sql/` 中增加稳定 SQL migration、视图和导出查询。
4. 增加 `export` 命令，输出面向 AI 训练的 JSONL/Parquet：paper metadata、composition JSONB、device stack、measurement metrics、evidence text、source asset hash。
