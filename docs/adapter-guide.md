# Web 框架接入指南

标准 HTTP、Fiber v3 和 Gin 示例将原始请求交给同一个 `server.Engine`，再按响应计划写出内容。应用可参考这些示例，把引擎挂载到已有路由与中间件之后。

## 示例入口

| 宿主      | 入口                                                    | 请求与响应实现                                                                  |
| --------- | ------------------------------------------------------- | ------------------------------------------------------------------------------- |
| 标准 HTTP | [examples/nethttp/main.go](../examples/nethttp/main.go) | [examples/internal/nethttp/handler.go](../examples/internal/nethttp/handler.go) |
| Fiber v3  | [examples/fiber3/main.go](../examples/fiber3/main.go)   | [examples/fiber3/handler.go](../examples/fiber3/handler.go)                     |
| Gin       | [examples/gin/main.go](../examples/gin/main.go)         | 通过 `gin.WrapH` 复用示例内部 HTTP Handler                                      |

标准 HTTP 与 Gin 共享的辅助包位于 `examples/internal/`。Fiber 在示例 `main` 包内使用私有函数 `ogcHandler`。宿主应用采用对应示例的接入方式，直接调用公共引擎与响应接口。

## 路由配置

以下片段用于仓库标准 HTTP 示例，`nethttp` 指示例内部辅助包：

```go
mux.Handle("/wms", nethttp.Handler(engine, server.Endpoint{
	Protocol: owscore.WMS,
}))

mux.Handle("/wmts", nethttp.Handler(engine, server.Endpoint{
	Protocol: owscore.WMTS,
}))

mux.Handle("/tiles/", nethttp.Handler(engine, server.Endpoint{
	Protocol: owscore.WMTS,
	Binding:  owscore.REST,
}))
```

共享 `/ows` 端点使用 `server.Endpoint{}`，由请求中的 `SERVICE` 选择协议。可信的 `Scope` 由 Endpoint 传入，供目录 Provider 选择资源视图。

## 接入层数据约定

| 字段                         | 取值方式                                         |
| ---------------------------- | ------------------------------------------------ |
| `Input.Method`               | 原始 HTTP 方法                                   |
| `Input.RawQuery`             | 保留空值、重复项和转义信息的原始 Query           |
| `Input.EscapedPath`          | 保留转义的请求路径                               |
| `Input.Protocol` / `Binding` | 宿主路由指定的协议与访问方式                     |
| `Input.Scope`                | 宿主确定的服务范围                               |
| Context                      | 生命周期覆盖业务回调与后续内容读取的标准 Context |

路径参数逐段解码，字面 `+` 保留为标识内容。提取池化框架的请求数据时，先取得本次请求自有的字符串，再交给引擎。

## 响应写出

接入层调用 `Engine.Handle` 获取 Plan，依次写出 `Status`、`Headers` 和 `Body`。内容读取或发送结束后执行 `Plan.Finish(bytesRead, sendErr)`。

标准 HTTP 示例在 Handler 内使用 `io.Copy` 同步发送。流传输中断后通过 `http.ErrAbortHandler` 终止本次响应；兜底清理覆盖 Reader panic 路径。

启用 `AllowHEAD` 后，HEAD 返回对应响应元信息，并执行内容关闭。Provider 可以沿用同一内容回调。

## Fiber 延迟流

Fiber 示例通过 `SendStream` 移交 Reader，框架在 Handler 返回后读取内容。`asyncBody` 统计读取字节与错误，使用 fasthttp 的 `CloseWithError` 完成 `Plan.Finish`。

Body 和请求 Context 的有效期覆盖实际发送过程，关闭责任随 Reader 一起移交给框架。升级 Fiber/fasthttp 后，运行 [延迟读取测试](../examples/fiber3/handler_test.go) 与共享 TCP 契约测试核对该生命周期。

## 宿主配置

认证、CORS、限流、追踪、恢复中间件和连接期限由应用配置。应用将协议响应交给专用接入层发送，并使用数据源的 Context 支持和连接写超时管理阻塞操作。

压缩中间件根据 Payload 的 `ContentEncoding` 协调输出编码。公开服务地址从可信配置传入 `WMSURL`、`WMTSURL` 和 `RESTBaseURL`。

Observer 的完成事件记录服务端传输结果，`BytesRead` 记录内容读取量。客户端确认逻辑可以由上层业务协议记录。

## 共享测试

接入测试通过 `adaptertest.Run(t, factory)` 执行。Factory 创建本地 TCP 服务，并登记关闭动作。共享用例覆盖版本、路由、元数据、像素查询、长度、HEAD、超时、客户端断开和资源释放。

```bash
go test -race ./examples/internal/nethttp
(cd examples/fiber3 && go test -race ./...)
(cd examples/gin && go test -race ./...)
```

以标准 `http.Handler` 为接口的宿主，可采用标准 HTTP 示例的请求提取与同步发送流程，并接入同一测试集。


## 资源式 API 的共享契约

`adaptertest.RunAPI` 在 net/http、Gin 和 Fiber 的真实 TCP 服务上运行同一套发现、要素、瓦片、地图、覆盖数据、同步/异步过程、作业、HEAD、304、204、大小限制和错误协商测试。

net/http 使用示例内部 `APIHandler`，Gin 通过 `gin.WrapH` 复用该实现。Fiber 使用示例私有 `apiHandler` 并与传统协议共用 `sendPlan`：请求字符串复制后使用，请求体只在 Handle 期间解码；响应流在发送完成后调用 Finish。204/304 不由框架补入默认 Content-Type，也不发送正文。
