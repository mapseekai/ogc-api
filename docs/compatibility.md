# 协议能力

本页列出 WMS / WMTS 的版本、操作和访问方式。能力表由 [compatibility.json](compatibility.json) 维护，`scripts/compatibility.py` 同步检查本页与 README。

<!-- compatibility:start -->

| 范围 | 版本 / 绑定 | 状态 |
| --- | --- | --- |
| WMS GetCapabilities / GetMap / GetFeatureInfo | 1.0.0、1.1.0、1.1.1、1.3.0 · GET KVP | 请求、目录、XML 与端到端测试 |
| WMTS GetCapabilities / GetTile / GetFeatureInfo | 1.0.0 · GET KVP / REST | 请求、目录、XML 与端到端测试 |
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

WMS `GetCapabilities` 按数值版本进行协商，内容请求按指定版本解析。WMTS `GetCapabilities` 使用 `AcceptVersions`，支持 `Sections`、`AcceptFormats` 和 `UpdateSequence` 参数处理。

## WMS 地图与查询

`GetMap` 解析有序图层和样式列表、坐标系、BBOX、宽高、格式、透明度和背景色。`GetFeatureInfo` 在地图请求基础上增加查询图层、像素列行、结果格式和要素数量。

`WireBBox` 保存请求中的轴顺序，`Bounds` 保存解释后的 XY 范围，`Normalized` 标识归一化状态。WMS 1.3.0 的内置轴信息覆盖 `EPSG:4326`、`CRS:84`、`EPSG:3857` 和实现中列出的标识别名。应用通过 `Catalog.Axes` 提供更多二维 CRS 的轴信息。

| 示例                       | BBOX 输入顺序 |
| -------------------------- | ------------- |
| WMS 1.1.1，`SRS=EPSG:4326` | 经度、纬度    |
| WMS 1.3.0，`CRS=EPSG:4326` | 纬度、经度    |
| WMS 1.3.0，`CRS=CRS:84`    | 经度、纬度    |

实现见 [wms/request.go](../wms/request.go)。

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
