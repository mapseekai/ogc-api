# 架构说明

## 目标与边界

同一份业务能力可以通过 WMS、WMTS KVP、WMTS REST 发布，也可以只调用解析函数。协议代码不认识 Fiber、Gin、数据库或地图引擎。

```text
Web 框架
  → examples/internal/nethttp 或 examples/fiber3 中的私有适配函数
  → server.Engine
  → kvp + wms / wmts
  → 不可变目录快照校验
  → server.Handlers 中的业务回调
  → response.Payload
  → response.Plan
  → 原框架发送、关闭 Body、结束请求上下文
```

## 包职责

| 包            | 职责                                                |
| ------------- | --------------------------------------------------- |
| `owscore`     | 协议标识、操作、版本、领域错误、保护限制、维度声明  |
| `kvp`         | 原始查询扫描、单次解码、重复字段、列表与类型读取    |
| `wms`         | 四版本请求、坐标轴语义、目录校验、Capabilities      |
| `wmts`        | KVP、编译后的 REST 模板、真实矩阵约束、Capabilities |
| `response`    | 内容载体、受控响应头、长度、关闭和取消责任          |
| `exception`   | 协议/版本/绑定对应的异常编码与状态映射              |
| `server`      | 获取快照、分发、回调、能力模式与生命周期观察        |
| `adaptertest` | 三种接入共用的真实 TCP 契约测试                     |

`internal/xmlutil` 只集中处理有界 XML 输出与转义，不提供通用模板系统。版本差异保留在对应协议的编码函数中。

## 示例与公共库的边界

Fiber 的 `main.go`、`handler.go` 和 `handler_test.go` 均位于独立示例模块 `examples/fiber3`，使用 `package main`。入口函数 `ogcHandler` 不导出；该目录用于演示接入，不是可导入、可单独发布的适配器包。Fiber 和 fasthttp 依赖只留在示例的 `go.mod`。

标准 HTTP 适配实现与测试放在 `examples/internal/nethttp`，由 `examples/nethttp` 和 `examples/gin` 共用。`Handler` 仅用于示例之间复用，Go 的 internal 导入边界禁止示例目录之外的业务项目直接引用它。公共库不再提供任何 HTTP 或第三方框架适配包，也不保留旧路径重导出。业务项目参考示例实现自己的接入层，调用 `server.Engine` 并写出 `response.Plan`。

## 三种使用模式

纯解析模式使用 `wms.Parse`、`wmts.Parse` 或 `wmts.Router.Match`，不要求目录或 Provider。请求包含原始参数及类型化字段；它不替代图层存在性、权限与矩阵限制校验。

完整引擎模式使用 `server.New`，为每个请求获取一次 `Snapshot`，基于这个快照校验并调用操作回调。

已有应用集成模式在鉴权中间件之后挂载适配器。主机应用决定路径、服务 Scope、用户身份与公开 URL，不由本库接管应用生命周期。

## 快照与并发

`server.NewSnapshot` 验证配置并深拷贝目录的 map、slice。内部字段不公开，后续更新通过创建新快照并替换指针完成。维度验证函数仍由业务持有，其闭包必须并发安全。

`CatalogProvider` 只调用一次。动态目录必须先根据当前用户过滤图层和矩阵集，再返回快照。不要让 Provider 在处理过程中再次读取一份可能变化的全局目录来覆盖已验证的请求语义。

引擎配置与 REST 模板在构造时确定。按需更换整个 Engine 或由 CatalogProvider 发布新快照，不提供动态插件注册中心。

## Ponytail 复用决策

参数扫描只在 `kvp` 中实现。REST 调用 `MatchParams` 复用已经扫描的查询，不再解析第二次。版本共性通过明确分支和小函数共享，没有为每个版本复制整套引擎。

响应长度、资源关闭与验证集中在 `response`；Fiber 和标准 HTTP 不各自实现业务错误识别。Gin 直接复用示例内部的标准 HTTP Handler，不创建只有一次转调的“Gin 协议内核”。

测试重用 `adaptertest.Run`，相同输入、状态与内容预期必须在所有适配器上成立。源码参考限定在协议部分，没有整体引入 go-tileproxy、MapProxy 或原生 GIS 库。

## 运行时成本

瓦片和地图内容不会整体读入内存。只有受控预编码 Capabilities 会在上限内读取以进行 XML 校验。普通图片流使用 `io.Reader` 向宿主框架传输；记录的 `BytesRead` 是服务端实际读取/交给传输的字节，不是客户端确认接收的字节。

输入长度、字段数、图层数、图片宽高和像素总数都有部署保护值。它们是本库的可配置资源约束，不是 OGC 协议规定的固定最大值。

## 异常与内容生命周期

引擎在调用任何外部 Observer 前建立清理责任。未成功返回 Plan 时，已接管的 Payload 或 Plan Body 仍由引擎关闭，Context 同时取消；不在异常清理时再次调用 Observer。正常返回后由接入层调用 `Plan.Finish`，原有只关闭一次的契约不变。

正常图片与瓦片沿用业务超时。库生成的有界错误文档使用父请求下独立的 1 秒内容读取窗口，因此业务超时不再阻断错误 XML；父请求取消仍立即生效。该窗口不是强制网络写超时，阻塞 Reader/Writer 仍由源实现和宿主连接期限控制。

WMTS 目录和 REST 编译共用维度保留字判断；Capabilities 通过完整目录共用矩阵可用范围、图层 Limits 和像素边界校验，不再宣告不存在的固定矩阵。业务自定义维度验证器仅在请求阶段调用，不在元数据发布时执行。

列表上限在按原始逗号计数后、展开分配前检查。REST 使用固定前后缀匹配单段占位符，仅在完整候选路径匹配后创建字段 Map，不引入新路由依赖或对象池。
