# ogc-api

面向 Go 的模块化 OGC 协议库，将标准接口与数据查询、地图渲染、瓦片读取和作业执行分开组织。

`ogc-api` 处理 WMS、WMTS 和资源式 OGC API 的请求解析、参数校验、资源描述与响应编码。应用通过小型 Go 接口接入后端能力，同一个瓦片 Reader 或地图 Renderer 可以服务于新旧协议。

核心模块使用 Go 标准库。net/http、Fiber 和 Gin 的接入示例集中在 `examples/`，便于与应用已有的路由、认证和连接管理组合。

[快速开始](#快速开始) · [使用示例](#使用示例) · [能力接口](#能力接口) · [架构](#架构) · [开发与验证](#开发与验证) · [文档](#文档)

## 协议与接入

<!-- compatibility:start -->

| 范围 | 版本 / 绑定 | 状态 |
| --- | --- | --- |
| WMS GetCapabilities / GetMap / GetFeatureInfo | 1.0.0、1.1.0、1.1.1、1.3.0 · GET KVP | 请求、目录、XML 与端到端测试 |
| WMTS GetCapabilities / GetTile / GetFeatureInfo | 1.0.0 · GET KVP / REST | 请求、目录、XML 与端到端测试 |
| OGC API Common | 1.0 · JSON 资源发现 / OpenAPI 3.0.3 文档 | 资源、快照、缓存与 HTTP 回归 |
| OGC API Features | Part 1 / 1.0.1 · 只读 GeoJSON 查询 | bbox、datetime、limit 截断、属性过滤、游标扩展、流式结果 |
| OGC API Features CRS | Part 2 / 1.0.1 · 显式多坐标系 profile | 原生轴序/维度、独立bbox/输出CRS、Content-Crs；实际转换由Reader负责 |
| OGC API Features Filtering | Part 3 / 1.0 · 有限 CQL2 请求 profile | Text/JSON typed AST、queryables、能力与资源校验；执行由 Reader 负责 |
| OGC API Tiles | Part 1 / 1.0 · 集合瓦片读取 / 规则矩阵 | 矩阵、Limits、格式协商、WMTS 共享后端 |
| OGC API Maps | 1.0 · 数据集/集合地图读取 profile | 集合选择、发现元数据、尺寸预算、CRS 元信息、WMS 共享后端 |
| OGC API Coverages | 19-087 / 1.0.0rc4 · 草案读取 profile | 覆盖读取、轴子集、字段选择、domainset/rangetype 与 schema |
| OGC API Processes | Part 1 / 1.0 · 执行与外部作业 profile | 同步 / 异步、raw / document / multipart、作业结果 |
| net/http | examples/nethttp 示例 | 真实 TCP 契约测试 |
| Fiber | v3.5.0 · examples/fiber3 示例 | 真实 TCP 与延迟流测试 |
| Gin | v1.12.0 · 共享 HTTP 接入示例 | 真实 TCP 契约测试 |

<!-- compatibility:end -->

集合、矩阵集、处理过程及其能力绑定构成服务目录。引擎根据目录生成资源链接、发现文档和 OpenAPI 描述。各协议的参数与表示见[协议能力说明](docs/compatibility.md)，Features Part 2 见[多坐标系契约与需求矩阵](docs/features-crs.md)，Part 3 的可选过滤能力见[过滤 profile 与需求矩阵](docs/features-filtering.md)。

## 环境准备

| 使用方式 | 环境 |
| --- | --- |
| 引用核心模块 | Go 1.24.0 及以上 |
| 运行源码工作区和示例 | Go 1.25.0 及以上 |
| 执行完整开发检查 | Go、Python 3、Make、Ruff、xmllint，以及项目固定的 goimports 和 Staticcheck |

`go.work` 管理核心模块、Fiber 示例和 Gin 示例的源码联调。工具安装与固定版本见[开发环境](docs/dependency-baseline.md)。

## 示例配置

组合示例通过以下环境变量配置监听地址和公开地址：

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `OGC_ADDR` | `127.0.0.1:8080` | 示例服务监听地址 |
| `OGC_PUBLIC_URL` | `http://` 加实际监听地址 | 资源链接使用的公开 HTTP(S) origin，例如 `https://maps.example.com` |

组合示例将资源式 API 挂载在 `/ogc/`，WMS、WMTS 分别挂载在 `/wms`、`/wmts`，并注册 WMTS REST 路径。

## 快速开始

在仓库根目录运行组合示例：

```bash
go run ./examples/ogcapi
```

示例包含一个内存点要素、按矩阵尺寸生成的 PNG 瓦片、地图绘制、单元格 CoverageJSON 和字符串处理过程 `echo`。

另开终端，读取服务首页和 OpenAPI 文档：

```bash
curl --fail 'http://127.0.0.1:8080/ogc/'
curl --fail 'http://127.0.0.1:8080/ogc/api'
```

服务首页返回资源发现链接，`/ogc/api` 返回当前目录的 OpenAPI 3.0.3 描述。

## 使用示例

### 查询要素

按空间范围读取 `world` 集合，并获取单个要素：

```bash
curl --fail 'http://127.0.0.1:8080/ogc/collections/world/items?bbox=-1,-1,1,1&limit=10'
curl --fail 'http://127.0.0.1:8080/ogc/collections/world/items/origin'
```

集合查询返回 GeoJSON `FeatureCollection`，单要素查询返回 GeoJSON `Feature`。要素查询可组合 `bbox`、`datetime` 和集合声明的属性条件；分页通过 `limit` 和后端提供的游标组织。

### 读取瓦片与地图

读取 `local` 矩阵集中的 `10m` 瓦片，得到 512 × 128 的 PNG：

```bash
curl --fail \
  'http://127.0.0.1:8080/ogc/collections/world/tiles/local/10m/1/1' \
  -o tile.png
```

同一瓦片后端也通过 WMTS REST 提供内容：

```bash
curl --fail \
  'http://127.0.0.1:8080/tiles/world/default/local/10m/1/1.png' \
  -o wmts-tile.png
```

按范围和输出尺寸绘制地图：

```bash
curl --fail \
  'http://127.0.0.1:8080/ogc/collections/world/map?bbox=-10,-5,10,5&width=400&height=200' \
  -o map.png
```

地图响应携带实际输出范围和坐标参考信息，分别通过 `Content-Bbox` 与 `Content-Crs` 表达。

### 读取覆盖数据

选择空间轴区间和温度字段：

```bash
curl --fail \
  'http://127.0.0.1:8080/ogc/collections/world/coverage?subset=x(0:1),y(0:1)&properties=temperature'
```

该示例返回包含温度字段的 CoverageJSON。覆盖数据接口将轴子集、字段、时间和坐标参考参数传递给 `coverages.Reader`。

### 执行处理过程

调用 `echo`，以 document 形式返回结果：

```bash
curl --fail \
  -H 'Content-Type: application/json' \
  -d '{"inputs":{"message":"hello"},"response":"document"}' \
  'http://127.0.0.1:8080/ogc/processes/echo/execution'
```

返回：

```json
{"result":"hello"}
```

处理能力由 `Validator`、`Executor` 和 `Submitter` 组合。配置外部异步提交与作业查询能力后，请求可通过 `Prefer: respond-async` 协商异步执行，获得作业状态与 `Location`。`JobReader`、`JobLister` 和 `Dismisser` 分别承接结果查询、列表与作业终止。

## 能力接口

| 能力 | 接口 | 实现职责 |
| --- | --- | --- |
| 要素 | `features.Reader`、`features.Iterator` | 查询集合与单个要素，逐项读取分页结果 |
| 瓦片 | `tiles.Reader` | 根据集合、矩阵集、矩阵、行列和格式读取或生成瓦片 |
| 地图 | `maps.Renderer` | 按请求绘制地图，返回内容、实际尺寸、CRS 和范围 |
| 覆盖数据 | `coverages.Reader` | 执行覆盖数据读取、轴子集与字段选择 |
| 同步处理 | `processes.Validator`、`processes.Executor` | 验证输入和输出选择，执行处理并交付结果 |
| 异步作业 | `processes.Submitter`、`processes.JobReader` | 提交外部作业，读取状态与结果 |
| 作业管理 | `processes.JobLister`、`processes.Dismisser` | 提供调用者可见的作业列表与终止操作 |

### 接入已有要素服务

以下函数接收应用实现的 `features.Reader`，将其发布为 `roads` 集合：

```go
package integration

import (
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func NewFeatureAPI(baseURL string, reader features.Reader) (*api.Engine, error) {
	snapshot, err := api.NewSnapshot(api.Catalog{
		Title:   "Roads API",
		BaseURL: baseURL,
		Collections: []api.Collection{{
			Metadata: common.Collection{ID: "roads", Title: "Roads"},
			Features: &features.Service{
				Reader: reader,
				Options: features.Options{
					DefaultLimit:     100,
					MaxLimit:         1000,
					FilterProperties: []string{"name"},
				},
			},
		}},
	})
	if err != nil {
		return nil, err
	}
	return api.New(api.Config{
		Catalog:       api.Static(snapshot),
		MaxConcurrent: 1024,
		Timeout:       10 * time.Second,
	})
}
```

`baseURL` 使用部署时的公开 API 地址，例如 `https://maps.example.com/ogc`。应用将 HTTP 请求转换为 `api.Input`，调用 `Engine.Handle`，再发送返回的 `response.Plan`。

完整接入代码见 [HTTP 示例辅助包](examples/internal/nethttp/handler.go)。Fiber 接入见 [Fiber 示例](examples/fiber3/handler.go)，Gin 使用标准 HTTP Handler 组合路由。

### 复用与替换后端

`tiles.WMTS` 将 `tiles.Reader` 接入 `server.Handlers.GetTile`；`maps.WMS` 将 `maps.Renderer` 接入 `server.Handlers.GetMap`。桥接保留原协议的矩阵标识、图层顺序、样式、维度和坐标轴信息。

资源式 API 通过 `api.AtomicCatalog.Publish` 同时发布新元数据和能力绑定，已经开始的请求继续使用原快照。组合 HTTP 示例还提供整代路由发布与旧请求排空，供宿主协调新旧协议后端的切换和资源回收。

## 架构

```text
宿主路由、身份认证与连接管理
                ↓
api.Engine                    server.Engine
资源式 OGC API                WMS / WMTS
                ↓
请求解析 → 目录快照 → 参数与能力校验 → 外部实现
                ↓
response.Payload → response.Plan → 宿主发送 → Plan.Finish
```

| 包或目录 | 职责 |
| --- | --- |
| `api`、`server` | 资源式 API 与传统协议的执行编排 |
| `common`、`owscore`、`kvp` | 公共模型、协议元信息、错误类型和参数解析 |
| `features`、`tiles`、`maps`、`coverages`、`processes` | 能力接口、请求语义与结果组织 |
| `wms`、`wmts` | 版本化请求、服务目录和能力文档 |
| `response`、`exception` | 响应所有权、流读取、完成回调和异常编码 |
| `internal` | 共享准入、JSON/XML 编码及内部协议桥接辅助逻辑 |
| `examples`、`adaptertest` | 宿主接入、演示后端与共享 TCP 契约测试 |
| `docs`、`scripts`、`testdata` | 接入文档、开发检查和固定测试夹具 |

### 目录与资源可见性

`api.NewSnapshot` 在发布阶段完成元数据复制、能力校验、矩阵索引、发现文档编码和 ETag 计算。请求取得一次目录快照，后续访问使用同一资源视图。

应用通过 `CatalogProvider(ctx, scope)` 返回按调用者权限组织的快照。`scope` 来自宿主的可信路由与认证上下文，后端实现承接具体数据访问授权。

### 并发与响应生命周期

`api.Config.MaxConcurrent` 和 `server.Config.MaxConcurrent` 均以 1024 为默认并发响应数，可按部署资源设置。准入名额覆盖目录读取、后端调用和正文传输，配合可配置的查询、文档、要素与图像预算组织资源使用。

Features 按需拉取并编码单个要素。瓦片、地图、覆盖数据和原始处理结果使用 `response.Payload` 传递内容；多输出处理使用 `multipart/related` 逐段读取。

宿主在发送结束或正文省略后调用 `Plan.Finish`，统一完成内容关闭、观察回调、Context 取消和名额归还。该生命周期覆盖 GET、HEAD、304、204，以及请求取消和传输结束路径。后端实现通过 Context 与宿主连接期限协作管理读取和计算。

`Observer` 提供请求进入、后端处理、响应交付、完成和准入事件。应用可据此记录耗时、状态和服务端读取字节数。对外协议消息与内部诊断分别保存，便于接入现有日志和监控系统。

## 开发与验证

准备 Python 3、Make 和 xmllint 后，在 Bash 环境安装开发工具并执行检查：

```bash
python3 -m venv .artifacts/tools-venv
.artifacts/tools-venv/bin/pip install ruff==0.9.10
export PATH="$PWD/.artifacts/tools-venv/bin:$PATH"

make tools
make test
./scripts/check.sh --consumers
```

`make test` 遍历源码工作区中的三个模块。完整检查覆盖 GoDoc、goimports、Staticcheck、`go vet`、普通测试、race、fuzz、纯 Go 构建、XML Schema/DTD、微基准与独立消费者，日志和汇总写入 `.artifacts/check/`。

net/http、Fiber 和 Gin 共用真实 TCP 契约测试。并发测试覆盖 2,000 个调用方的响应持有与名额回收，以及目录替换和路由排空。微基准分别测量元数据访问、瓦片读取与要素编码的耗时和内存分配。

```bash
go test -run '^$' \
  -bench 'Benchmark(MetadataParallel|TileParallel|CollectionStream)$' \
  -benchmem ./api ./features
```

测试环境、命令和结果见[验证记录](docs/fix-review-20261014.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [资源式 API 接入](docs/ogc-api.md) | 目录构造、路由、能力接口和结果交付 |
| [WMS/WMTS 接入](docs/provider-guide.md) | 传统协议目录、业务回调、维度与能力文档 |
| [HTTP 框架接入](docs/adapter-guide.md) | net/http、Fiber、Gin 与传输生命周期 |
| [架构说明](docs/architecture.md) | 包职责、依赖关系与请求流程 |
| [并发与资源预算](docs/high-concurrency.md) | 快照发布、准入、流式传输和性能测量 |
| [协议能力](docs/compatibility.md) | 协议版本、参数和资源表示 |
| [代码规范](docs/code-style.md) | 全局与 Go 规范、文件布局和同名测试约定 |
| [开发环境](docs/dependency-baseline.md) | 工具版本、工作区与检查命令 |
| [标准来源](docs/standards-decisions.md) | 协议语义、标准文档与固定 Schema |
| [变更记录](CHANGELOG.md) | 版本变更与公共接口更新 |
