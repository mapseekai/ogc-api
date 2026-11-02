# 条件读取与空结果接入

此契约仅组织请求、协议响应与资源所有权，不提供内容缓存、压缩器、存储或授权系统。

## 输入与兼容

宿主把 `If-None-Match` 与 `Accept-Encoding` 传给 `server.Input.IfNoneMatch` / `AcceptEncoding` 和 `api.Input` 同名字段。多个字段行按逗号组合；Fiber 必须复制借用字符串。旧协议的 `MaxHeaderBytes` 默认 16 KiB，限制这两个字段与 Accept 的合计长度；API 使用既有总头预算。

引擎将 `response.Conditions` 放入 WMS 地图、WMTS 瓦片和共享 `tiles.Request` / `maps.Request`。`tiles.WMTS`、`maps.WMS` 保留这些值。它们不是 KVP 参数，不由查询字符串覆盖。Reader/Renderer 的方法签名与旧回调不变；新增字段零值保持普通内容行为。

## 明确结果

| Payload.State | 要求与结果 |
| --- | --- |
| `response.Content`（零值） | 与过去相同，Body 非 nil；零字节仍是正常 200，不推断成 204。条件命中时转换 304。 |
| `response.NotModified` | Body nil、Length 0；后端已完成授权、版本核查和条件判断，不必获取完整正文。仅接受带 If-None-Match 的请求，结果 304。 |
| `response.NoContent` | Body nil、Length 0；明确合法空结果，仅显式开启的 profile 返回 204。 |

`Payload.Validate` 保持普通内容校验，拒绝非 Content 状态；需要上述读取结果的调用方使用 `ValidateResult` 并声明策略。未知状态、冲突正文/长度、格式不符或后端错误均走错误路径，不能变成空瓦片。

`server.Config.AllowNoContent` 仅允许 WMS GetMap 与 WMTS GetTile（KVP/REST）；默认关闭，不能用于 Capabilities / FeatureInfo。资源式 API 仅 `tiles.Service.AllowNoContent` 在具体 tileset 上启用 204；API Maps 不允许 204，但与 WMS 共享的 Renderer 可以报告 NotModified（304），此时无需渲染图片或提供图片尺寸/BBox/CRS。其他资源式能力仍保持原契约。此为宿主兼容策略，**不宣称 WMTS 标准一律要求 204**。

```go
payload := response.Payload{
	State: response.NotModified,
	Cache: response.Cache{ETag: `"content-style-version"`, CacheControl: "private"},
	Vary:  "Authorization, X-API-Key, Accept-Encoding",
}
```

## 授权、版本与并发变化

先执行目录可见性、协议/坐标校验及业务回调，之后才做本地弱 ETag 比较或 `*` 存在性判断。缓存命中不是授权捷径。后端报告 NotModified 也必须已经确认其条件与当前表示一致；库不能替代后端的版本/授权策略。

`server.Config.BeforeResponse` / `api.Config.BeforeResponse` 仅在 GET/HEAD 读取后端成功后、交付前复核当前权限、停服状态与内容/样式版本；即使结果为 204/304 仍执行。返回错误就拒绝原成功结果并释放其资源。该读取钩子不用于 POST/DELETE 等写入或过程执行，避免副作用成功后再反转结果；这些操作的授权仍走原有后端路径。闭包可通过请求 Context 读取宿主传入的身份、资源和版本信息；不要依赖共享可变的“当前请求”。版本变化时应返回错误或由宿主重新发起整个请求，不能把旧 ETag 改为新 ETag 后继续发送旧正文。

此钩子不是数据库事务或网络交付锁：校验到发送之间的变化仍由宿主的一致性模型处理；需要更强保证时，应持有资源版本/租约，并在延迟 Reader 内协作验证与取消。引擎不会在流发送前过早关闭正文。

## 响应头与生命周期

200 保留 Content-Type、Content-Encoding、长度与缓存元数据。204/304 不发送正文、不发送 Content-Length 或 Content-Type；304 保留所选表示的 Content-Encoding、ETag、Last-Modified、Cache-Control 与 Vary。库不解压或再次压缩 MVT/MLT；后端负责按 Accept-Encoding 选择表示以及正确的编码/ETag。

Payload.Vary 与协议 Accept / Accept-Encoding 合并；示例适配器进一步与宿主已设置的 Vary 合并，保留 Authorization、X-API-Key 等维度（大小写不敏感去重，`*` 优先）。宿主仍应设置适合私有数据的 Cache-Control；Vary 不是授权机制。

条件命中的普通内容不读取正文，并关闭被丢弃的流；正常 200 延迟流保持存活至发送结束。宿主必须对成功、HEAD、204、304、取消、读写错误都调用 `Plan.Finish`，归还并发名额并取消 Context；重复 Finish 不会重复释放。示例 net/http、Gin 和 Fiber 复用真实 TCP 契约覆盖这些路径。

HTTP 条件顺序、弱比较及无正文语义依据 [RFC 9110 §§13.1.2、13.2.1、15.3.5、15.4.5](https://www.rfc-editor.org/rfc/rfc9110.html)。生产消费方迁移和联调不包含在本库实现验收中。
