# Provider 接入指南

## 最小接入顺序

构造 WMS 或 WMTS 的目录，调用 `server.NewSnapshot`。为需要的内容操作注册函数，然后用 `server.New` 创建引擎，并挂载一个 Web 适配器。

`examples/demo.Config` 是完整可运行的起点。实际项目应替换目录与 Handler，而不是修改 `wms`、`wmts` 或框架适配器。

```go
handlers := server.Handlers{
	GetTile: func(ctx context.Context, request *wmts.TileRequest) (response.Payload, error) {
		// tileSource 是业务已有的数据读取能力，不属于 ogc-api。
		body, size, err := tileSource.Open(ctx, request.TileMatrixSet,
			request.TileMatrix, request.TileRow, request.TileCol)
		if err != nil {
			return response.Payload{}, err
		}
		return response.Stream(request.Format, body, size), nil
	},
}
```

上面的 `tileSource` 是业务接口示意，不是 SDK 内置对象。可以改成文件、PMTiles、缓存或对象存储；本库不要求引入某种存储实现。完整可编译示例在 `examples/demo/demo.go`。

## 四个独立内容回调

| 回调                 | 请求内容                                 | 返回内容             |
| -------------------- | ---------------------------------------- | -------------------- |
| `GetMap`             | 有序图层、样式、明确轴序范围、尺寸与格式 | 已生成的地图图像     |
| `GetTile`            | 图层、样式、矩阵集、矩阵标识、行列与维度 | 已生成或读取到的瓦片 |
| `GetWMSFeatureInfo`  | 地图请求、查询图层、统一像素列行         | 已有查询结果         |
| `GetWMTSFeatureInfo` | 瓦片选择、统一像素列行与查询格式         | 已有查询结果         |

只发布瓦片的应用不需要实现 WMS 回调。没有注册查询回调时，Managed Capabilities 不声明对应查询操作。

## 内容所有权

使用 `response.Bytes(contentType, data)` 或 `response.Stream(contentType, body, length)` 构造内容。`length=-1` 表示未知，`0` 表示空内容，二者不能混用。

Provider 返回 Payload 后，所有权移交给库。不要再读取、关闭或修改这份内容；也不要在 Provider 中 `defer body.Close()`。成功输出、HEAD、校验失败、写入失败或“同时返回内容与错误”的防御路径都会关闭被接管的 Body。

`Bytes` 不额外复制字节切片。静态嵌入资源可以只读共享；复用缓冲池中的字节必须保证直到发送完成都不会被其他请求修改。

Reader 应遵守 Go 的 Reader 契约。需要及时中断的源应使用 Context 感知的读取实现。普通阻塞 Reader 无法被库强制打断，宿主应同时配置连接写超时。

## 元信息

```go
payload := response.Stream("image/png", body, response.UnknownLength)
payload.Cache.ETag = `"tile-revision-42"`
payload.Cache.CacheControl = "public, max-age=3600"
return payload, nil
```

上述缓存元信息只是受控输出，不代表本库实现了缓存存储、ETag 条件请求或 Range。媒体类型不匹配与响应头注入会在发送前被拒绝。

## 目录和授权

`CatalogProvider(ctx, scope)` 返回已经按用户权限过滤的不可变快照。`Scope` 由路由或宿主传入，不从 URL 的 `scope`、`tenant` 等字段取值。用户身份应存放在标准 Context 中，使用宿主定义的类型安全 key。

使用 `atomic.Pointer[server.Snapshot]` 或宿主现有缓存保存当前快照。数据更新时创建新 Snapshot 再替换引用，不修改正在被并发请求使用的 map。`server/engine_test.go` 包含并发替换和快照隔离测试。

图层名、矩阵名和维度值只是标识，不应该直接拼接本地路径或 SQL。Provider 自己承担存储层的访问控制与输入映射责任。

## 维度

声明 `Dimension.Values`、`Default` 与相关标志。WMS 多图层的解析结果在 `MapRequest.LayerDimensions` 中分别保存；WMTS 结果在 `TileRequest.Dimensions` 中。

区间和近邻逻辑使用 `Validate(context.Context, string) (string, error)`。回调返回业务接受的表达式，不必生成全部时间步。校验函数闭包应是并发安全的。

## 能力文档

默认 Managed 模式从同一份目录生成版本化 XML。公开 URL 必须是可信配置，不能直接拷贝未经校验的 Host 或 Forwarded，更不能带用户 Token。

Encoded 模式必须显式设置 `CapabilitiesMode=server.Encoded` 并注册 `EncodedCapabilities`。它收到 `server.CapabilitiesRequest`，包含选定版本及 WMS/WMTS 的具体请求。库检查字节上限、内容类型、XML 根节点、命名空间与版本；目录一致性、所有公开链接和完整 Schema 内容仍由业务负责。

目录声明 `UpdateSequence` 时，同时提供 `CompareUpdateSequence(requested, current)`。它应返回负数、零、正数或错误，不假设修订号是整数。WMS 与 WMTS 的“未改变”响应语义不同，由引擎处理。

## 错误和观测

返回 `owscore.Fail(kind, locator, safeMessage)` 可以表达允许对外显示的协议错误。普通内部 error 的字符串不会直接公开；Observer 的 Error 记录实际回调/传输错误，ProtocolError 保留协议错误分类，避免把 HTTP 200 的 WMS 异常误判为业务成功。

Observer 的阶段为 `start`、`business`、`handoff`、`finish`。解析或校验失败不一定到达 business。finish 表示服务端传输路径结束与资源清理，不表示客户端已确认收到数据。Observer 必须线程安全，不应 panic，也不要记录完整授权 URL 或返回内容。

## 审查修复后的补充契约

`Config.Timeout` 约束正常业务与内容读取。发生协议或内部错误时，预生成错误文档在父请求下获得独立的 1 秒读取窗口；父请求已取消时不会绕过取消发送内容。网络写超时仍由宿主配置。不要在错误路径用 `context.Background()` 替代用户请求来屏蔽取消。

Observer 的 `business` 或 `handoff` 阶段 panic 会继续向宿主传播，但未交付的内容会先被关闭，请求 Context 也会取消。未实际交付的 Plan 不再额外触发 `finish` 事件，避免在清理路径再次进入故障 Observer。已经交付的 Plan 仍由接入层完成；`finish` Observer 自身 panic 时也会取消 Context。

`Catalog.ValidateMap`、`ValidateTile`、`ValidateInfo` 会填充维度结果，WMS 查询还可能选取默认 `InfoFormat`。它们不是只读校验函数：每个请求应独占，失败后丢弃部分填充结果，不能多 goroutine 共享写入。

`response.Bytes` 不复制切片；发送结束前不可归还缓冲池或修改内容。`response.Plan` 含同步状态，使用后不能按值复制，也不能在 `OnFinish` 中递归调用 `Finish`。
