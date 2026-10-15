# 协议能力

本页列出 WMS / WMTS 的版本、操作和访问方式。能力表由 [compatibility.json](compatibility.json) 维护，`scripts/compatibility.py` 同步检查本页与 README。

<!-- compatibility:start -->

| 范围 | 版本 / 绑定 | 状态 |
| --- | --- | --- |
| WMS GetCapabilities / GetMap / GetFeatureInfo | 1.0.0、1.1.0、1.1.1、1.3.0 · GET KVP | 请求、目录、XML 与端到端测试 |
| WMTS GetCapabilities / GetTile / GetFeatureInfo | 1.0.0 · GET KVP / REST | 请求、目录、XML 与端到端测试 |
| OGC API Common | 1.0 · JSON 资源发现 / OpenAPI 3.0.3 文档 | 资源、快照、缓存与 HTTP 回归 |
| OGC API Features | Part 1 / 1.0.1 · 只读 GeoJSON 查询 | bbox、datetime、limit 截断、属性过滤、游标扩展、流式结果 |
| OGC API Tiles | Part 1 / 1.0 · 集合瓦片读取 / 规则矩阵 | 矩阵、Limits、格式协商、WMTS 共享后端 |
| OGC API Maps | 1.0 · 数据集/集合地图读取 profile | 集合选择、发现元数据、尺寸预算、CRS 元信息、WMS 共享后端 |
| OGC API Coverages | 19-087 / 1.0.0rc4 · 草案读取 profile | 覆盖读取、轴子集、字段选择、domainset/rangetype 与 schema |
| OGC API Processes | Part 1 / 1.0 · 执行与外部作业 profile | 同步 / 异步、raw / document / multipart、作业结果 |
| net/http | examples/nethttp 示例 | 真实 TCP 契约测试 |
| Fiber | v3.5.0 · examples/fiber3 示例 | 真实 TCP 与延迟流测试 |
| Gin | v1.12.0 · 共享 HTTP 接入示例 | 真实 TCP 契约测试 |

<!-- compatibility:end -->

## 版本与请求参数

| 版本             | 参数与编码规则                                                            |
| ---------------- | ------------------------------------------------------------------------- |
| WMS 1.0.0        | `WMTVER`，`map` / `capabilities` / `feature_info`，旧格式名称及版本化 XML |
| WMS 1.1.0、1.1.1 | `SRS`、`X/Y`，对应版本的 Capabilities 和异常文档                          |
| WMS 1.3.0        | `CRS`、`I/J`，按 CRS 轴序解释 BBOX                                        |
| WMTS 1.0.0       | KVP 与 REST，字符串矩阵标识、行列号及维度                                 |

WMS `GetCapabilities` 按数值版本进行协商，内容请求按指定版本解析。WMTS `GetCapabilities` 使用 `AcceptVersions`，支持 `Sections`、`AcceptFormats` 和 `UpdateSequence` 参数处理。`Sections=all` 作为 `All` 的兼容别名接受，其他节名仍按规范拼写校验。

同时提供 `VERSION` 和 `WMTVER` 时，以 `VERSION` 为准。WMS 1.1.1 接受历史 `map` / `feature_info` 操作别名。WMTS KVP `GetFeatureInfo` 必须提供原瓦片的 `FORMAT`；REST 信息资源使用模板固定的 `INFOFORMAT`。宿主应把 HTTP Accept 传给 `server.Input.Accept`，以便 WMTS REST 返回正确的406与 Vary。

## WMS 地图与查询

`GetMap` 解析有序图层和样式列表、坐标系、BBOX、宽高、格式、透明度和背景色。`GetFeatureInfo` 在地图请求基础上增加查询图层、像素列行、结果格式和要素数量。

`WireBBox` 保存请求中的轴顺序，`Bounds` 保存解释后的 XY 范围，`Normalized` 标识归一化状态。WMS 1.3.0 的内置轴信息覆盖 `EPSG:4326`、`CRS:84`、`EPSG:3857` 和实现中列出的标识别名。应用通过 `Catalog.Axes` 提供更多二维 CRS 的轴信息。

| 示例                       | BBOX 输入顺序 |
| -------------------------- | ------------- |
| WMS 1.1.1，`SRS=EPSG:4326` | 经度、纬度    |
| WMS 1.3.0，`CRS=EPSG:4326` | 纬度、经度    |
| WMS 1.3.0，`CRS=CRS:84`    | 经度、纬度    |

实现见 [wms/request.go](../wms/request.go)。

`Layer.BoundingBoxes` 接收各 CRS 下的 XY 边界框，编码器按版本和轴序输出。已声明的地理 CRS 可使用 `GeographicBounds`；投影 CRS 不做隐式重投影，须由宿主提供边界框。根图层广告公共 CRS 和合并后的地理范围。

## WMTS 矩阵与路径

`TileMatrixSet` 与 `TileMatrix` 保留字符串标识。目录按 `MatrixWidth`、`MatrixHeight` 和图层 `MatrixLink.Limits` 检查行列，按 `TileWidth`、`TileHeight` 检查查询像素。

REST 模板支持每个路径段一个 `{Name}` 占位符，并可带固定前后缀。格式通过 `Fixed` 指定；额外维度通过 `Dimensions` 声明。查询参数与路径绑定值执行一致性校验。

`Router.Resources(layerID, catalog, baseURL, featureInfo)` 根据完整目录生成资源地址，检查固定样式、矩阵、范围和格式。能力文档与请求匹配共用模板。

实现见 [wmts/rest.go](../wmts/rest.go) 和 [wmts/catalog.go](../wmts/catalog.go)。

## 维度

维度支持枚举值、默认值和 `current`；WMTS 还识别 `default` 选择。WMS 多值维度、时间区间及近邻选择通过声明与 `Dimension.Validate` 回调组合。

WMS 1.1.0、1.1.1、1.3.0 具有对应的维度 XML 结构。带维度服务可通过 `wms.Options.Versions` 选择这三个版本。WMTS 每次瓦片请求选择一个维度值，结果保存在 `TileRequest.Dimensions`。

维度名使用协议保留字之外的标识，例如 `Time`、`Elevation` 或业务自定义名称。目录与 REST 使用同一保留字规则。

## 参数和资源限额

参数名进行 ASCII 大小写折叠，标识值保留原样。解析层区分缺失值、空值和具体值，保留原始参数。图层和样式按原始逗号切分，`%2C` 作为标识内容保留。

`owscore.Limits` 提供部署级限额，零值字段采用以下默认值：

| 配置项                             |         默认值 |
| ---------------------------------- | -------------: |
| `MaxQueryBytes`                    |    65,536 字节 |
| `MaxPathBytes`                     |    16,384 字节 |
| `MaxParameters`                    |            256 |
| `MaxLayers`                        |             64 |
| `MaxImageWidth` / `MaxImageHeight` |  各 8,192 像素 |
| `MaxImagePixels`                   |     16,777,216 |
| `MaxCapabilitiesBytes`             | 8,388,608 字节 |

`LAYERS`、`STYLES`、`QUERY_LAYERS` 在列表展开前检查数量。默认值定义在 [owscore/types.go](../owscore/types.go)。

## 内容与异常

业务回调提供图片、瓦片和查询内容，引擎校验媒体类型并组织响应。内容支持字节切片、已知长度流、动态长度流和空内容。

响应元信息包括 `Content-Type`、`Content-Encoding`、`Content-Length`、`ETag`、`Last-Modified` 和 `Cache-Control`。GET 用于内容获取，HEAD 可通过配置启用。

WMS 1.0.0 的显式 XML 异常使用 `EXCEPTIONS=WMS_XML`，根节点为 `WMTException`；默认图像异常模式采用空错误内容路径。WMS 1.1.x 和 1.3.0 使用各自的 `ServiceExceptionReport` 编码。WMTS 输出 OWS `ExceptionReport`，状态码结合操作和访问方式确定。

内部诊断与面向客户端的消息分别处理，协议结果与传输结果分别保存在 Plan 和观察事件中。


## 资源式 OGC API 的精确范围

- Common：资源发现、JSON、缓存和 OpenAPI 文档；生成文档与完整 OAS conformance 分开记录。
- Features：Part1/1.0.1只读查询，CRS84、四/六维BBox、时间交集、显式属性过滤、limit截断；cursor为扩展。写操作、CQL和Part2 CRS不是本次范围。
- Tiles：集合瓦片、规则二维矩阵、字符串矩阵ID、独立像素尺寸和矩阵Limits。可变矩阵宽度、动态重分块不在当前规则网格模型内。
- Maps：数据集与集合地图、collections 有序选择、bbox/scaling 参数及返回元信息。Catalog.Map 显式启用数据集端点，限于已发布集合地图；渲染、重投影和编码仍由 Renderer 执行，不自动声明全部 Maps 类。默认 CRS84 地图省略可选 Content-Crs，其他 CRS 保留带尖括号 URI。地图发现链接通过 Accept 协商表示，旧 f 参数请求仍可使用。
- Coverages：19-087/1.0.0rc4 草案读取、轴子集、字段选择、schema，以及成对发布的 CIS DomainSet/RangeType 资源；不将草案开发套件通过宣称为正式标准认证。
- Processes：标准执行文档、同步/异步能力、raw/document/multipart、结果引用、外部作业状态与结果；作业列表为limit/cursor profile。回调、完整作业过滤类和自动reference输入抓取没有在本次实现。

版本来源、协议边界和生命周期契约见 [资源式 API 接入](ogc-api.md)。新增异步能力必须同时提供外部JobReader，目录不根据空接口或未绑定能力发布路由。

Processes 原始多输出由执行文档选择各输出格式，并按 Part 1 要求29返回 `multipart/related`，不以外层 Accept 改变该固定封装。单输出和 document 仍校验 Accept。引擎编码的异常支持协商 `application/json` / `application/problem+json`，默认保留后者，并返回 `Vary: Accept`。
