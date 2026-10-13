# 业务接入指南

业务方通过目录描述可访问的资源，通过 `server.Handlers` 提供地图、瓦片和查询内容。协议引擎把客户端参数转换为经过目录校验的请求对象。

## 构造一个 WMTS 引擎

下面的完整 Go 包构造 `world` 图层、`local` 矩阵集和 `L00` 矩阵。`readTile` 由调用方注入，可连接文件、缓存或对象存储读取逻辑。

```go
package integration

import (
	"context"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wmts"
)

func NewTileEngine(
	readTile func(context.Context, *wmts.TileRequest) (response.Payload, error),
) (*server.Engine, error) {
	catalog := wmts.Catalog{
		Service: owscore.Service{Title: "Tile service"},
		Layers: map[string]wmts.Layer{
			"world": {
				Title:        "World",
				DefaultStyle: "default",
				Styles: map[string]owscore.Style{
					"default": {Title: "Default"},
				},
				Formats: []string{"image/png"},
				MatrixSets: map[string]wmts.MatrixLink{
					"local": {},
				},
			},
		},
		MatrixSets: map[string]wmts.MatrixSet{
			"local": {
				Title: "Local grid",
				CRS:   "urn:ogc:def:crs:EPSG::3857",
				Matrices: map[string]wmts.Matrix{
					"L00": {
						ScaleDenominator: 1000,
						TopLeftCorner:    [2]float64{0, 1000},
						TileWidth:        256,
						TileHeight:       256,
						MatrixWidth:      3,
						MatrixHeight:     2,
					},
				},
			},
		},
	}
	snapshot, err := server.NewSnapshot(nil, &catalog)
	if err != nil {
		return nil, err
	}
	return server.New(server.Config{
		Catalog:  server.Static(snapshot),
		WMTSURL:  "http://127.0.0.1:18080/wmts",
		Handlers: server.Handlers{GetTile: readTile},
	})
}
```

调用时传入实际的 `readTile` 函数，`WMTSURL` 使用部署服务的公开地址。WMS 服务采用相同接入流程，目录类型为 `wms.Catalog`，基础内容回调为 `GetMap`。同时发布两种服务时，把两份目录交给同一个 `NewSnapshot`。

仓库中的 [demo.Config](../examples/demo/demo.go) 展示双协议、REST 模板和四个内容回调的组合。

## 内容回调

| 回调                 | 请求类型                   | 主要请求内容                            |
| -------------------- | -------------------------- | --------------------------------------- |
| `GetMap`             | `*wms.MapRequest`          | 图层、样式、CRS、范围、宽高、格式与维度 |
| `GetWMSFeatureInfo`  | `*wms.FeatureInfoRequest`  | 地图请求、查询图层、像素位置、结果格式  |
| `GetTile`            | `*wmts.TileRequest`        | 图层、样式、矩阵集、矩阵、行列与维度    |
| `GetWMTSFeatureInfo` | `*wmts.FeatureInfoRequest` | 瓦片选择、像素位置与结果格式            |

各回调接收标准 `context.Context`，返回 `(response.Payload, error)`。按服务用途注册对应回调，Managed Capabilities 结合目录与回调配置声明可用操作。

## 字节和流内容

```go
package integration

import (
	"io"

	"github.com/mapseekai/ogc-api/response"
)

func PNGBytes(data []byte) response.Payload {
	return response.Bytes("image/png", data)
}

func PNGStream(body io.ReadCloser, size int64) response.Payload {
	payload := response.Stream("image/png", body, size)
	payload.Cache.ETag = `"tile-revision-42"`
	payload.Cache.CacheControl = "public, max-age=3600"
	return payload
}
```

长度为实际字节数时，Plan 输出 `Content-Length`。`response.UnknownLength`（`-1`）表示动态长度流；`0` 表示空内容。`Cache` 还可以携带 `LastModified`，Payload 可以携带 `ContentEncoding`。

媒体类型和响应头字段在引擎中校验，宿主按 Plan 输出。缓存策略、连接管理和业务数据读取可复用应用现有组件。

## 所有权与关闭顺序

Provider 在返回 Payload 时移交内容所有权，随后由引擎与接入层负责读取和关闭。接入层在正常完成、HEAD、发送错误等结束路径调用 `Plan.Finish`，由它完成关闭、观察事件和 Context 取消。

`response.Bytes` 直接引用传入切片。字节内容在响应完成前保持只读；缓冲池中的内存于完成后归还。静态嵌入资源可按只读方式共享。

Plan 含同步状态，始终通过指针传递。先完成内容读取，再执行 Finish。`OnFinish` 承担结果观察或业务清理，Finish 的调用由接入层统一管理。

Reader 遵循 Go 的读取约定，数据源使用 Context 感知实现配合取消；宿主配置 `WriteTimeout` 等连接期限。

## 目录与授权

`CatalogProvider(ctx, scope)` 返回按调用者权限选择的 Snapshot。路由或宿主提供可信的 Scope；用户身份可通过 Context 中的应用类型传递。

`server.Static` 用于向调用者提供同一份目录。动态目录可通过 `atomic.Pointer[server.Snapshot]` 或已有业务缓存切换快照。更新时先构造并验证新 Snapshot，再替换当前引用。

业务方将图层名、矩阵名等标识映射为已授权的数据源，以存储组件的接口执行读取。

## 维度与请求状态

`owscore.Dimension` 描述单位、默认值、可选值以及 `Current`、`MultipleValues`、`NearestValue` 等规则。自定义时间区间和近邻选择通过 `Validate(context.Context, string)` 实现。

WMS 校验把结果写入 `MapRequest.LayerDimensions`，按图层分别保存默认值；WMTS 写入 `TileRequest.Dimensions`。WMS 查询校验还会按目录补充默认 `InfoFormat`。请求对象在单次调用中独占，校验成功后交给业务处理；校验失败时释放该次请求状态。

共享维度验证闭包使用并发安全的业务数据。

## 能力文档

`server.Managed` 是默认模式，从目录与已注册操作生成 Capabilities。`server.Encoded` 模式配合 `EncodedCapabilities` 回调，接收业务提供的预编码 XML。

Encoded 回调获得 `server.CapabilitiesRequest`，包括协议、选定版本和协议专属参数。引擎验证内容类型、字节上限、XML 根节点、命名空间与版本。业务方维护预编码文档中的目录、公开链接和 Schema 内容。

配置中的公开 URL 使用可信的服务地址。目录声明 `UpdateSequence` 时，通过 `CompareUpdateSequence(requested, current)` 提供修订顺序，返回负数、零、正数或错误。

## 错误、超时与观察事件

业务错误通过 `owscore.Fail(kind, locator, safeMessage)` 提供面向客户端的消息。内部错误由引擎转换为受控异常文档，完整诊断由观察接口处理。

`Config.Timeout` 覆盖目录、回调和正常内容读取。错误文档在父请求下获得独立的 1 秒读取窗口，同时继承父请求取消。连接写期限由宿主维护。

| 事件       | 记录内容                 |
| ---------- | ------------------------ |
| `start`    | 请求进入引擎             |
| `business` | 内容处理阶段完成         |
| `handoff`  | 响应计划交付接入层       |
| `finish`   | 服务端传输结束与资源清理 |

Observer 使用并发安全、及时返回的实现。`ProtocolError` 保存协议结果，`Error` 记录处理和传输结果，`BytesRead` 表示服务端内容读取字节数。日志中使用请求标识与受控元信息。

外部钩子发生 panic 时，引擎先释放仍由自己持有的内容，再把异常交给宿主。已交付响应的清理由接入层完成。
