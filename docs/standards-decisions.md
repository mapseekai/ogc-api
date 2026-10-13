# 标准语义与实现来源

协议行为由 WMS、WMTS 的版本规则和仓库实现共同说明。本页给出主要语义、源码位置以及标准与参考项目入口。

## 标准入口

| 协议 | 版本  | 标准标识     |
| ---- | ----- | ------------ |
| WMS  | 1.0.0 | OGC 00-028   |
| WMS  | 1.1.0 | OGC 01-047r2 |
| WMS  | 1.1.1 | OGC 01-068r3 |
| WMS  | 1.3.0 | OGC 06-042   |
| WMTS | 1.0.0 | OGC 07-057r7 |

官方标准与发布材料：[WMS](https://www.ogc.org/standards/wms/)、[WMTS](https://www.ogc.org/standards/wmts/)。仓库固定的 XML 定义及文件摘要见 [schemas.lock.json](schemas.lock.json)。

## 参数与列表

参数名使用 ASCII 大小写折叠，图层、样式和矩阵等标识值按原样保存。`kvp.Params` 保留原始 Query，支持单值、列表与完整参数集合读取。

图层和样式列表在原始字面逗号处分隔，再解码各项。`LAYERS=a%2Cb,c` 对应 `a,b` 与 `c` 两个图层标识。`ListLimit` 在展开前检查元素数量，样式列表保留空项和位置关系。

BBOX 先解码，再读取四个有限数值。版本和整数使用独立的数值解析规则，WMS 1.3.0 的 `FEATURE_COUNT` 按该版本规则采用默认值 1。

实现：[kvp/params.go](../kvp/params.go)、[wms/request.go](../wms/request.go)。

## 坐标轴与矩阵

WMS 1.3.0 的轴序由具体 CRS 确定。`EPSG:4326` 采用纬度、经度的传输顺序，`CRS:84` 采用经度、纬度。`WireBBox`、`Bounds` 和 `Normalized` 分别记录原始范围、解释后的范围与处理状态。

轴信息可通过 `Catalog.Axes` 扩充。业务方使用明确轴序的请求范围执行数据读取或地图处理。

WMTS 的 `TileMatrix` 是目录中的字符串键。瓦片位置由矩阵宽高与图层 Limits 共同约束，查询像素由瓦片宽高约束。矩阵访问与资源宣告复用同一组校验函数。

实现：[wms/catalog.go](../wms/catalog.go)、[wmts/catalog.go](../wmts/catalog.go)。

## 版本与能力文档

WMS Capabilities 根据数字版本顺序协商；内容操作使用明确版本的请求模型。WMTS Capabilities 通过 `AcceptVersions` 处理版本选择，通过 `Sections` 选择文档部分。

历史 WMS 编码器分别组织操作节点、格式节点、OnlineResource 和异常结构。WMS 1.1.1 维度使用 `Dimension` / `Extent`，其中 Extent 输出 `default` 和 `nearestValue`；WMS 1.3.0 使用对应 Dimension 属性。

公开地址由可信配置提供。WMTS 的 `ResourceURL` 与解析路由共用模板，按完整目录选择适用资源。

实现：[wms/capabilities.go](../wms/capabilities.go)、[wmts/capabilities.go](../wmts/capabilities.go)、[wmts/rest.go](../wmts/rest.go)。

## 异常与响应

WMS 1.0.0 的 `WMS_XML` 文档使用 `WMTException`。WMS 1.1.x / 1.3.0 采用版本化 `ServiceExceptionReport`，WMTS 采用 OWS `ExceptionReport`。

异常编码按协议、版本和访问方式选择状态码与 XML。`owscore.Error` 分别保存客户端消息和内部原因，`Plan.ProtocolError` 记录协议结果。

内容由 Payload 交给 Plan，宿主执行发送。`Finish` 保证一次关闭与完成回调，错误文档的独立读取窗口继承父请求取消。

实现：[exception/encode.go](../exception/encode.go)、[response/plan.go](../response/plan.go)、[server/engine.go](../server/engine.go)。

## 参考代码

| 项目                                                             | 对照内容                                     |
| ---------------------------------------------------------------- | -------------------------------------------- |
| [MapProxy](https://github.com/mapproxy/mapproxy)                 | WMS 历史版本、请求规则、WMTS 路径和 XML 组织 |
| [go-tileproxy](https://github.com/flywave/go-tileproxy)          | Go 请求模型和 WMS/WMTS 操作处理              |
| [ogc-osgeo](https://github.com/flywave/ogc-osgeo)                | OGC XML 与坐标标识模型                       |
| [ogc-specifications](https://github.com/PDOK/ogc-specifications) | 协议结构与参数解析                           |

源码对照记录使用 commit 和文件 SHA-256 固定在 [references.lock.json](references.lock.json)。协议实现采用当前仓库的 Go 模型、标准库编码和共享规则，运行依赖由各模块清单记录。

## XML 验证

测试使用仓库内固定的 Schema/DTD，文件保留上游内容与声明。`scripts/validate_xml.py` 通过本地 XML Catalog 和 `xmllint --nonet` 完成版本化校验。

WMS 1.0.0 异常采用 XML 良构性验证，其他夹具按对应 DTD/XSD 校验。验证记录包含普通能力文档、带维度能力文档和异常文档，共 14 份黄金 XML。
