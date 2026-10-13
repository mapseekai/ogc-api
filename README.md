# ogc-api

Go WMS / WMTS 协议库，提供 URL 参数解析、版本处理、服务目录校验、Capabilities 与异常文档编码，以及字节和流响应模型。业务方通过类型化回调接入地图、瓦片和查询数据。

[快速开始](#快速开始) · [使用方式](#使用方式) · [API 参考](#api-参考) · [架构说明](docs/architecture.md) · [测试记录](docs/test-report.md)

## 项目简介

`ogc-api` 将客户端请求转换为 Go 请求对象，再结合服务目录检查图层、样式、坐标系、矩阵集和维度。`server.Engine` 负责操作分发，业务回调返回 `response.Payload`，宿主接入层根据 `response.Plan` 写出 HTTP 响应。

核心模块使用 Go 标准库。标准 HTTP、Fiber v3 和 Gin 的接入实现集中在 `examples/`，共用协议引擎、目录模型和内容回调。

## 核心能力

<!-- compatibility:start -->

| 范围                                            | 版本 / 绑定                          | 状态                         |
| ----------------------------------------------- | ------------------------------------ | ---------------------------- |
| WMS GetCapabilities / GetMap / GetFeatureInfo   | 1.0.0、1.1.0、1.1.1、1.3.0 · GET KVP | 请求、目录、XML 与端到端测试 |
| WMTS GetCapabilities / GetTile / GetFeatureInfo | 1.0.0 · GET KVP / REST               | 请求、目录、XML 与端到端测试 |
| net/http                                        | examples/nethttp 示例                | 真实 TCP 契约测试            |
| Fiber                                           | v3.5.0 · examples/fiber3 示例        | 真实 TCP 与延迟流测试        |
| Gin                                             | v1.12.0 · 共享 HTTP 接入示例         | 真实 TCP 契约测试            |

<!-- compatibility:end -->

参数解析保留原始 Query、参数空值、列表顺序和转义标识。WMS 请求同时提供原始 BBOX 与按坐标轴顺序解释后的范围；WMTS 使用字符串矩阵标识，并按目录校验瓦片行列和图层 Limits。

响应模型支持字节切片、已知长度流、动态长度流、缓存元信息和可配置的 HEAD。`Observer` 提供请求开始、业务完成、响应交付和传输结束事件。

完整版本规则见 [协议能力](docs/compatibility.md)。

## 环境要求

| 用途                       | 项目配置                                     |
| -------------------------- | -------------------------------------------- |
| 核心模块                   | `go.mod` 声明 Go 1.24.0                      |
| 本地工作区、Fiber/Gin 示例 | `go.work` 和示例模块声明 Go 1.25.0           |
| Go 分析工具                | `make tools` 使用 Go 1.27.0 构建固定版本工具 |
| 本文验证环境               | Go 1.27.0、macOS、ARM64                      |

格式、静态分析和 XML 校验工具见 [开发环境](docs/dependency-baseline.md)。

## 快速开始

使用具有仓库访问权限的 GitHub SSH 账户获取源码：

```bash
git clone git@github.com:mapseekai/ogc-api.git
cd ogc-api
```

启动标准 HTTP 示例：

```bash
OGC_ADDR=127.0.0.1:18080 go run ./examples/nethttp
```

在另一个终端请求服务目录和瓦片：

```bash
# WMS 1.3.0 服务目录
curl --fail \
  'http://127.0.0.1:18080/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities'

# WMTS REST 瓦片
curl --fail \
  'http://127.0.0.1:18080/tiles/world/default/local/L00/0/0.png' \
  -o tile.png

# WMTS REST 像素查询
curl --fail \
  'http://127.0.0.1:18080/info/world/default/local/10m/1/1/127/511.json'
```

示例使用 `world` 图层、`local` 矩阵集和仓库内嵌的 PNG/JSON 夹具。上面的查询返回：

```json
{ "type": "FeatureCollection", "features": [] }
```

`OGC_ADDR` 的默认值为 `127.0.0.1:8080`。也可以选择 Fiber 或 Gin 示例，分别运行：

```bash
(cd examples/fiber3 && OGC_ADDR=127.0.0.1:18081 go run .)
```

```bash
(cd examples/gin && OGC_ADDR=127.0.0.1:18082 go run .)
```

三种示例的路由和内容回调保持一致。请求地址使用对应的监听端口。

## 使用方式

### 解析 URL 参数

```go
package main

import (
	"fmt"
	"log"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wmts"
)

func main() {
	request, err := wmts.Parse(
		"SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=world"+
			"&STYLE=default&FORMAT=image/png&TILEMATRIXSET=local"+
			"&TILEMATRIX=L00&TILEROW=0&TILECOL=0",
		owscore.Limits{},
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("matrix=%s row=%d col=%d\n",
		request.Tile.TileMatrix, request.Tile.TileRow, request.Tile.TileCol)
}
```

输出为 `matrix=L00 row=0 col=0`。WMS 的对应入口是 `wms.Parse(rawQuery, wms.Options{})`；WMTS 路径请求通过 `wmts.Router.Match` 解析。

### 接入业务服务

构造 `wms.Catalog` 或 `wmts.Catalog`，通过 `server.NewSnapshot` 创建目录快照。注册 `server.Handlers` 后，使用 `server.New` 创建引擎。

内容回调接收经过校验的请求对象，返回 `response.Bytes` 或 `response.Stream`。接入层将原始 HTTP 输入交给 `Engine.Handle`，写出 Plan 的状态、响应头和内容，并在传输结束时执行 `Plan.Finish`。

完整代码见 [业务接入](docs/provider-guide.md)；请求提取和响应写出见 [框架接入](docs/adapter-guide.md)。

## API 参考

| 入口                                 | 用途                                         |
| ------------------------------------ | -------------------------------------------- |
| `kvp.Parse`                          | 解析原始查询参数，保留空值、重复项和转义信息 |
| `wms.Parse`                          | 解析 WMS 操作、版本、地图范围和像素查询参数  |
| `wmts.Parse` / `wmts.Router.Match`   | 解析 KVP 或 REST 形式的 WMTS 请求            |
| `server.NewSnapshot`                 | 验证并复制服务目录                           |
| `server.New` / `Engine.Handle`       | 创建引擎、分发请求并生成响应计划             |
| `response.Bytes` / `response.Stream` | 构造字节或流内容                             |
| `Plan.Finish`                        | 关闭内容、汇总传输结果并完成生命周期回调     |

在仓库根目录查看 GoDoc：

```bash
go doc ./server.Config
go doc ./wms.MapRequest
go doc ./wmts.TileRequest
go doc ./response.Plan
```

## 架构

```text
HTTP / Fiber / Gin 接入层
          ↓
     server.Engine
          ↓
  请求解析 → 目录快照校验 → 业务回调
          ↓
  response.Payload → response.Plan
          ↓
    宿主写出与资源关闭
```

| 目录                                 | 内容                                      |
| ------------------------------------ | ----------------------------------------- |
| `kvp/`、`owscore/`                   | 参数读取、公共协议类型、维度和资源限额    |
| `wms/`、`wmts/`                      | 请求、服务目录、Capabilities 与 REST 模板 |
| `server/`、`response/`、`exception/` | 请求编排、内容生命周期与协议异常          |
| `examples/`                          | 三种接入示例及共享业务夹具                |
| `adaptertest/`                       | 跨框架真实 TCP 契约测试                   |
| `scripts/`、`testdata/`              | 工程检查、官方 Schema 与测试夹具          |

## 验证与测试

准备 [开发工具](docs/dependency-baseline.md) 后执行：

```bash
make tools
pnpm install --frozen-lockfile
make fmt
./scripts/check.sh --consumers
```

检查脚本遍历三个 Go 模块，执行格式、GoDoc、Staticcheck、`go vet`、单元测试、race、`CGO_ENABLED=0` 构建、四组 fuzz smoke、XML 校验、微基准和独立消费者验证。

[2026-10-13 的本地测试记录](docs/test-report.md) 包含 26 个通过的检查组、14 份 XML 校验和 128 项公共声明的 GoDoc 检查。结果对应 Go 1.27.0 / macOS ARM64 环境，具体命令和数据口径随报告记录。

## 文档

| 文档                                      | 阅读内容                               |
| ----------------------------------------- | -------------------------------------- |
| [协议能力](docs/compatibility.md)         | 版本、操作、访问方式与参数语义         |
| [业务接入](docs/provider-guide.md)        | 目录、内容回调、维度、授权与响应所有权 |
| [框架接入](docs/adapter-guide.md)         | 标准 HTTP、Fiber、Gin 的路由与流发送   |
| [架构说明](docs/architecture.md)          | 包依赖、目录快照和执行生命周期         |
| [开发环境](docs/dependency-baseline.md)   | 工具版本、工作区与工程命令             |
| [标准与来源](docs/standards-decisions.md) | 实现规则、官方标准和参考源码           |
| [测试记录](docs/test-report.md)           | 本地验证环境、命令和检查结果           |
| [Go 质量检查](docs/go-review.md)          | 编码规范、资源管理、并发和性能验证     |
| [CITE 接入指南](docs/cite.md)             | 测试服务准备与官方测试套件入口         |

代码变更见 [CHANGELOG.md](CHANGELOG.md)。
