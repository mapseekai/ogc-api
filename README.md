# ogc-api

一个框架无关的 Go WMS / WMTS 协议适配库。业务方提供地图、瓦片、查询结果与服务目录；本库负责 URL 参数解析、版本规则、校验、操作分发、Capabilities、异常和响应模型。HTTP 响应的实际写出由宿主接入层负责。

项目名称是 `ogc-api`，当前实现的是 WMS / WMTS，不代表实现 OGC API — Features、Tiles、Maps 等另一组标准。

## 能力范围

<!-- compatibility:start -->

| 范围                                            | 版本 / 绑定                          | 状态                              |
| ----------------------------------------------- | ------------------------------------ | --------------------------------- |
| WMS GetCapabilities / GetMap / GetFeatureInfo   | 1.0.0、1.1.0、1.1.1、1.3.0 · GET KVP | 已实现、已内部测试                |
| WMTS GetCapabilities / GetTile / GetFeatureInfo | 1.0.0 · GET KVP / REST               | 已实现、已内部测试                |
| net/http                                        | examples/nethttp 示例                | 示例已实现、真实 TCP 测试         |
| Fiber                                           | v3.5.0 · examples/fiber3 示例        | 示例已实现、真实 TCP 与延迟流测试 |
| Gin                                             | v1.12.0 · 共用示例内部 HTTP 接入     | 示例已实现、共享契约测试          |
| 完整 OGC CITE / 认证                            | 真实业务 Provider 与标准数据集       | 未执行、不宣称认证                |

<!-- compatibility:end -->

独立解析不依赖 Web 框架，核心模块只使用 Go 标准库。所有 HTTP 与框架接入代码都在 `examples/`，不作为公共库 API 发布。标准 HTTP 和 Gin 共用 `examples/internal/nethttp`；该包只能在示例目录内引用，外部项目应参考源码实现自己的接入层。Fiber 的私有适配函数与框架依赖只留在 `examples/fiber3` 示例模块。三种示例共用同一引擎和业务回调，没有复制协议解析代码。没有地图渲染、重投影、瓦片生成、数据库、对象存储或代理抓取实现。

## 本地运行

工作区需要 Go 1.25 或更高版本；核心模块单独使用时最低为 Go 1.24。下面的三个示例使用相同的业务回调，都只监听 `127.0.0.1:8080`，一次启动一个。

```bash
# 标准 HTTP 示例
go run ./examples/nethttp

# Fiber v3 示例
(cd examples/fiber3 && go run .)

# Gin 示例
(cd examples/gin && go run .)
```

如果 8080 已被其他服务占用，可以只调整示例监听地址：

```bash
OGC_ADDR=127.0.0.1:18080 go run ./examples/nethttp
```

使用自定义端口时，请把请求地址中的端口同步修改。默认端口的请求示例：

```bash
curl 'http://127.0.0.1:8080/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities'

curl 'http://127.0.0.1:8080/wmts?SERVICE=WMTS&REQUEST=GetCapabilities'

curl 'http://127.0.0.1:8080/tiles/world/default/local/L00/0/0.png' -o tile.png

curl 'http://127.0.0.1:8080/info/world/default/local/10m/1/1/127/511.json'
```

示例返回仓库内的固定 PNG 和 JSON，用于验证协议接入，不会依据任意 BBOX 渲染地图，也不是完整 CITE 地图测试数据集。

## 只解析，不启动服务

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
	fmt.Println(request.Tile.TileMatrix) // L00，不转换为 zoom。
}
```

WMS 使用 `wms.Parse(rawQuery, wms.Options{})`。完整服务使用 `server.New`、不可变目录快照和 `server.Handlers`。完整可运行接入见 [Provider 指南](docs/provider-guide.md) 和 `examples/`。

## 开发与检查

```bash
pnpm install --frozen-lockfile
make tools

# Go 源码及导入分组、Markdown、JSON、YAML 格式化。
make fmt

# 遍历全部 Go module：格式、GoDoc、Staticcheck、vet、单测、race、无 CGO 构建、fuzz、XML。
make check

# 额外验证未发布模块的独立消费，不使用 go.work 或永久本地 replace。
make consumers

# 更新依赖清单；临时替换不会写入正式 go.mod。
make tidy
```

Python 脚本格式检查需要 `ruff 0.9.10`，可以用 `uv tool install ruff==0.9.10` 安装到独立工具环境。XML 校验需要 `xmllint`；macOS 通常自带，Linux 可安装 `libxml2-utils`。官方 Schema/DTD 已固定在 `testdata/schemas/`，日常校验不访问网络。格式工具仅为开发依赖，不进入 Go 模块。`make tools` 把固定版本的 goimports 和 Staticcheck 安装在 `.artifacts/tools`，使用 Go 1.27.0 构建分析工具，不替换全局工具或更改核心 Go 版本。

## 模块与发布状态

模块路径为 `github.com/mapseekai/ogc-api`，当前为开发版本，尚未发布版本标签。示例中的 `v0.0.0` 仅用于本地工作区联调，不能将 `go get ...@v0.0.0` 当成公开可用的安装命令。

`go.work` 中的版本限定相对替换用于本地联调；所有发布用 `go.mod` 都不含个人目录替换。`scripts/test_consumers.py` 会建立临时文件模块代理，验证公共核心离开工作区也能独立构建，并确认外部消费者不能导入示例内部的 HTTP 适配包。正式发布前应确认仓库归属、许可证和核心版本，并更新示例中的 `v0.0.0` 开发占位版本。

## 文档

本轮 Go 审查的 8 项发现已修复，关闭依据见 [修复与回归记录](docs/go-review-fixes.md)。

| 文档                                          | 内容                                     |
| --------------------------------------------- | ---------------------------------------- |
| [架构说明](docs/architecture.md)              | 包依赖、执行流程、简洁性与复用决策       |
| [兼容矩阵](docs/compatibility.md)             | 已实现范围、明确限制、版本差异           |
| [业务接入](docs/provider-guide.md)            | 目录、回调、权限范围、流响应             |
| [框架适配](docs/adapter-guide.md)             | net/http、Fiber、Gin 与新框架接入        |
| [标准与实现决策](docs/standards-decisions.md) | 轴序、历史版本、参数编码、错误规则       |
| [依赖基线](docs/dependency-baseline.md)       | Go、框架和工具的固定版本                 |
| [测试报告](docs/test-report.md)               | 实际执行证据与未执行验证                 |
| [Go 专项审查](docs/go-review.md)              | 生命周期缺陷、架构、规范、性能与实测证据 |
| [CITE 联合验证](docs/cite.md)                 | 真实业务 Provider 的一致性验证入口       |
| [第三方来源说明](THIRD_PARTY_NOTICES.md)      | 参考源码、许可证与 Schema 来源           |

**本库不宣称通过 OGC 认证。** 单元测试、跨框架契约测试及 XML 结构校验，不等同于完整地图服务的一致性认证。
