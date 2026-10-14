# 资源式 OGC API 接入

`api.Engine` 解释协议，`api.Snapshot` 持有公开资源与能力绑定，外部实现负责实际数据和计算。它与现有 `server.Engine` 并存，共用 `response.Payload`、`response.Plan` 和明确的关闭契约。

## 最小接入

以下函数接收业务方已经实现的要素 Reader，构造独立于 HTTP 框架的协议引擎。

```go
package application

import (
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func NewFeatureAPI(publicURL string, reader features.Reader) (*api.Engine, error) {
	snapshot, err := api.NewSnapshot(api.Catalog{
		Title:   "Spatial data service",
		BaseURL: publicURL,
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
		MaxConcurrent: 2048,
		Timeout:       10 * time.Second,
	})
}
```

`BaseURL` 必须是受信任的外部绝对地址，可包含挂载前缀，禁止查询串、用户凭据和 fragment。宿主把原始 `EscapedPath`、`RawQuery`、标准请求头和请求 Body 交给 `Handle`。完整 `net/http` 实现在 `examples/internal/nethttp/handler.go`；该目录只作为接入示例，不是公共 HTTP 依赖。

宿主必须保留输入字符串直到 `Plan.Finish`。使用复用请求缓冲区的框架时，复制需要留给流或 Observer 的字符串。Body 的所有权仍属于 HTTP 宿主。

## 可执行路由

| 路由 | 方法 | 启用条件 |
| --- | --- | --- |
| `/`、`/conformance`、`/api` | GET / HEAD | 有效目录 |
| `/collections`、`/collections/{id}` | GET / HEAD | 发布 Collection |
| `/collections/{id}/items`、`/items/{featureId}` | GET / HEAD | `features.Reader` |
| `/tileMatrixSets`、`/tileMatrixSets/{id}` | GET / HEAD | 发布规则矩阵集 |
| `/collections/{id}/tiles`、`/tiles/{tms}` | GET / HEAD | `tiles.Service` |
| `/collections/{id}/tiles/{tms}/{matrix}/{row}/{col}` | GET / HEAD | `tiles.Reader` |
| `/collections/{id}/map` | GET / HEAD | `maps.Renderer` |
| `/collections/{id}/coverage`、可选 `/schema` | GET / HEAD | `coverages.Reader`、可选 Schema |
| `/processes`、`/processes/{id}` | GET / HEAD | 发布 Process |
| `/processes/{id}/execution` | POST | `Executor` 或 `Submitter`，并配置 `Validator` |
| `/jobs/{id}`、`/jobs/{id}/results` | GET / HEAD | `JobReader` |
| `/jobs` | GET / HEAD | `JobLister` |
| `/jobs/{id}` | DELETE | `Dismisser` |

仅存在接口类型不产生路由；目录绑定了实际能力才发布对应资源。`/api` 生成当前快照的 OpenAPI 3.0.3 描述。

## 要素查询

`features.Reader` 将 `Items` 返回的 Iterator 所有权转交给协议层，单个 `Item` 返回一个要素。`bbox`、`datetime` 和显式声明的属性谓词共同生效。CRS84 经度允许跨越反经线；空几何/无时间几何的标准筛选语义由 Reader 正确执行。

`limit` 默认 10，最大默认 1000，可按集合配置。正整数超过最大值时按上限截断，包含超出 uint64 表示范围的十进制请求；零、负数、非整数仍拒绝。规则依据 Features 1.0.1 `/req/core/fc-limit-response-1`。

`cursor` 是显式扩展，内容由 Reader 解释；`Page.NextCursor` 生成后续链接。未知查询参数返回 400，避免过滤条件被静默忽略。

`Next(ctx)` 每次交付一个独占要素，结束使用 `io.EOF`。几何与属性是 JSON 值；拓扑有效性、几何编码、空间查询及坐标转换由实现负责。后端应遵守请求 Limit 和单个要素字节预算。流在读取时编码，不预读整页。

## 新旧协议复用

`tiles.Reader.ReadTile` 接收字符串矩阵标识、uint64 行列、格式和资源标识。它返回 `response.Bytes` 或 `response.Stream`。WMTS 桥接保留样式和维度：

```go
handlers.GetTile = tiles.WMTS(tileReader)
handlers.GetMap = maps.WMS(mapRenderer, limits)
```

WMTS 目录和新 Tiles 目录各自处理标准语义，复用同一后端能力。`tiles.FromWMTS` 转换规则矩阵元数据时，调用方必须明确提供 CRS 的 `metersPerUnit`。矩阵 ID 不会转换为隐式数字 zoom。

地图桥接保留 WMS 的线性层顺序、样式、维度与已解释的 XY 轴标记。新 Maps profile 中，仅给定 width 或 height 时，缺失维度由 Renderer 保持像素纵横比计算；它必须在分配输出前执行传入尺寸预算，并返回实际尺寸、CRS、BBox。

## 覆盖数据 profile

本次固定为 **OGC 19-087 / 1.0.0rc4 草案读取 profile**。参数为 `bbox`、`bbox-crs`、`datetime`、`subset`、`subset-crs`、`properties`、`crs` 和 `f`。

`subset=x(1:10),time("2026-10-14T00:00:00Z")` 表达区间与切片。轴和字段必须先在 Service 中声明。协议层解析语法，不执行 GDAL、重采样或文件解码。`Schema` 是外部提供的范围字段元数据。

## 处理与作业

每个 Process 必须提供 `Validator`，在执行前校验输入 Schema 实例、基数和输出格式；协议层先处理标准文档结构、重复键、参数名、必填输入、输出选择和传输模式。未知 `mode`、回调选项等不会被静默接受。

`Executor` 是同步能力，`Submitter` 是外部异步提交能力。二者可以分别实现。请求通过 `Prefer: respond-async` 选择已发布的异步能力；同步专用服务忽略不适用的偏好。异步提交返回 201、`Location` 和作业状态。异步执行必须脱离提交请求 Context，由外部系统负责持久化、并发限制和恢复。

| 返回选择 | 协议表示 |
| --- | --- |
| 单个 raw 值 | 原始 JSON 或内容流 |
| 多个 raw 值 | `multipart/related`，逐段读取并关闭各内容 |
| 全部 reference | 204 与结果 Link 头 |
| document | 有预算的 JSON 输出文档；对象值及二进制内容使用限定表示 |
| 作业尚未完成 | 404 与标准 `result-not-ready` 异常类型 |

默认 `response` 为 **raw**。文档结果上限默认 8 MiB；大栅格或大型结果优先使用 raw 流或外部 reference。带额外 Content-Encoding 的二进制内容应使用 raw 或 reference，避免协议层解压计算。

协议库不自动获取输入/reference URL，不创建调度线程或作业数据库。外部实现应执行自己的 URL 访问策略、授权和资源配额。作业 Reader 必须先按当前调用者过滤资源；结果中的 ProcessID 还会与本次快照的可见 Process 集合核对。

## 原子替换后端

```go
var publication api.AtomicCatalog
if err := publication.Publish(initialSnapshot); err != nil {
	return err
}
engine, err := api.New(api.Config{Catalog: publication.Load})
if err != nil {
	return err
}
_ = engine

next, err := api.NewSnapshot(catalogWithReplacementProviders)
if err != nil {
	return err
}
return publication.Publish(next)
```

发布同时更换元数据和 Provider 绑定，已经开始的请求持有原快照。旧数据库连接池、客户端及其他后端资源的退役由宿主负责，不能在 Publish 后立即关闭仍被请求使用的实现。

`AtomicCatalog` 是单一可见域，不等于授权系统。多租户应用提供 `CatalogProvider(ctx, scope)`，按可信路由和身份选择已过滤的快照，不从查询参数推断租户。发现文档默认 `Cache-Control: private, no-cache`。

## 响应、错误和状态

所有 Payload/Iterator/Results 在返回时转移所有权，**包括与 error 同时返回的部分内容**。宿主应在成功、HEAD、304、取消、发送异常等所有路径调用一次 `Plan.Finish`。再次调用可得到同一结束结果，不重复关闭。

`common.ErrNotFound`、`ErrForbidden`、`ErrUnavailable` 等稳定错误类别可跨后端复用；私有错误 Cause 只进入诊断，客户端得到安全的协议描述。WMS/WMTS 桥接自动转换相同类别。

`OnFinish` 是宿主回调，不能替换协议引擎受保护的生命周期。回调或外部 Close 的 panic 仍向宿主传播，取消和并发配额回收继续执行。

## Conformance 的范围

Common 发现、JSON 与所绑定的 Features/Tiles 基础能力生成相应声明。API 的实际符合性还取决于外部实现正确履行契约。

生成 OpenAPI 文档不等于完成完整 OAS conformance class。当前未生成 HTML `service-doc`，因此不自动宣告该完整类。Maps、Coverages 和 Processes 的本次 profile 也不因为存在路由就宣告全部标准类。

本次没有运行完整官方 CITE。标准 Schema、协议契约和本地测试是实现证据，不是认证。逐协议的剩余范围与版本边界见 [协议能力](compatibility.md)。


## 严格协议边界与公共接口调整

`common.ValidID` 统一资源发布和请求路由的标识约束：支持 Unicode、非数字矩阵标识和转义斜杠；空标识、点段、控制字符和无效 UTF-8 在边界拒绝。集合元数据先验证原始标识，再进行编码复制。

`features.Feature.ID` 接受 string、json.Number 及 Go 内建整数/浮点类型；任意自定义编码器和指针不作为 ID。`processes.Service.Prepare(maxDescriptionBytes int)` 要求正数描述预算；`api.NewSnapshot` 自动使用剩余目录预算调用它，不保留旧签名兼容层。

执行文档显式给出 `response` 或 `transmissionMode` 时必须使用允许的字符串枚举。`format.mediaType`、`encoding` 不能为 null，`format.schema` 为字符串或对象。业务 `inputs` 是否允许 null 仍由各过程的 Schema 验证器决定。

Processes OpenAPI 的 JSON 原始结果允许标量、数组、对象及 null，二进制和 multipart 使用 binary Schema，引用结果以带 Link 的 204 描述。同步执行与作业结果复用同一编码、格式协商和失败清理路径。作业撤销结果必须保留已校验的 JobID 和 ProcessID。

所有结果内容均尝试关闭；单个 Close panic 不会跳过其他输出，完成清理后向宿主传播原异常。原始流和 multipart 保持惰性读取，单次元数据及 document 编码受预算约束。
