# 兼容性与边界

兼容范围由 `compatibility.json` 维护，`scripts/compatibility.py` 校验本页与 README 的表格一致。

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

这里的“已实现、已内部测试”指该版本列出的 URL 请求、目录校验、能力文档、异常和样例端到端路径。完整业务服务的 OGC 一致性认证尚未执行。

## 版本与操作差异

| 项目              | 实现规则                                                               |
| ----------------- | ---------------------------------------------------------------------- |
| WMS 1.0.0         | `WMTVER`、`map`、`capabilities`、`feature_info`、旧格式名、旧 XML 结构 |
| WMS 1.1.0 / 1.1.1 | `SRS`、`X/Y`，分别输出对应的能力和异常文档                             |
| WMS 1.3.0         | `CRS`、`I/J`，依据具体 CRS 处理坐标轴顺序                              |
| WMS Capabilities  | 数字版本协商；普通 GetMap 不静默降级                                   |
| WMTS Capabilities | `AcceptVersions` 协商；`Sections` 与不透明 `UpdateSequence`            |
| WMTS TileMatrix   | 保留字符串，不解释成整数 zoom                                          |
| WMTS 矩阵约束     | 使用目录中的行列数、瓦片宽高与图层 Limits                              |
| WMTS REST         | 单段一个 `{Name}` 占位符，可有字面前后缀；不是完整 RFC 6570            |

## 内容与错误

地图、瓦片和查询内容都由业务回调提供。库校验响应媒体类型与请求匹配，但不会解码图片或验证渲染结果的地理正确性。

现代 WMS 默认提供 XML 异常，不内置 INIMAGE/BLANK 图片错误渲染。WMS 1.0.0 未指定 `EXCEPTIONS` 时采用标准允许的空错误响应路径；需要可读异常时显式传 `EXCEPTIONS=WMS_XML`。该版本的错误根元素是 `WMTException`，不是现代的 `ServiceExceptionReport`。

WMTS KVP 越界响应为 400；REST 资源越界为 404。异常文档不泄漏底层错误的私有路径、SQL 或堆栈。宿主鉴权层可以在进入协议处理前完成 401/403；目录隐藏资源时按不存在处理。

## 坐标与维度

内置轴信息覆盖常见的 EPSG:4326、CRS:84、EPSG:3857 及部分明确列出的标识形式。更多 CRS 应通过目录 `Axes` 注册。未知轴顺序在纯解析模式中明确标记为未归一化；完整服务目录需要明确轴信息。

维度的枚举、默认值、`current` 和 WMTS `default` 关键字由本库处理。区间、复杂时间表达式、近邻选择必须提供 `Dimension.Validate`，并不会无限展开时间序列。WMTS 一次瓦片请求选择一个维度值。WMS 多图层默认值按图层分别解析。

WMS 1.0.0 没有本库现代维度模型的标准化能力声明路径；带有维度的服务应将该版本从 `wms.Options.Versions` 排除，而不是让库输出不符合旧 DTD 的字段。

## HTTP 与框架

支持 GET，以及配置启用的 HEAD。支持已知长度、未知长度、空内容和流式 Body。返回受控 Content-Type、Content-Encoding、ETag、Last-Modified 和 Cache-Control 元信息。

不自动提供 Range、条件请求协商、缓存存储或跨域策略，也不宣告 Accept-Ranges。若宿主启用压缩中间件，必须避免对已经有 Content-Encoding 的业务内容二次压缩，并重新执行流契约测试。

`net/http` 具有其原生请求取消机制；Fiber 不承诺客户端断开即取消业务上下文。Fiber 使用真实写入失败与流关闭信号清理，并支持协作式超时。不会为阻塞 Reader 的每次 Read 创建 goroutine 来伪造强制中断。

## 后续范围

GetLegendGraphic、POST/SOAP、Fiber v2 和更多框架接入示例属于后续范围，当前不创建空实现，也不预建第三方框架公共适配包。Gin 已有实际接入和共享测试；其他接受 `http.Handler` 的宿主可参考标准 HTTP 示例编写接入层，但未逐一执行框架兼容验证。所有 HTTP 与框架适配实现均为示例，不作为公共库 API。

## 本轮行为修正

WMTS 保留字规则对目录与 REST 一致，`ACCEPTVERSIONS`、`SECTIONS`、`ACCEPTFORMATS`、`UPDATESEQUENCE` 及原有协议字段不能再声明为维度名（ASCII 大小写不敏感）。使用这些名称的配置应重命名。

WMS 的 `LAYERS`、`STYLES`、`QUERY_LAYERS` 按 `MaxLayers` 在列表展开前受控拒绝，仍保留 `%2C`、空样式项、列表顺序和单次解码规则。

`wmts.Router.Resources` 现在接收完整 `wmts.Catalog`，而不是单独 `wmts.Layer`，以核实固定矩阵、图层 Limits、行列与像素边界。此为未发布 API 的签名调整；模板语法与协议版本没有扩大。
