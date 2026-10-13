# Web 框架适配指南

## 标准 HTTP 与 Gin 示例

`examples/internal/nethttp` 中的 `Handler(engine, endpoint)` 返回标准 `http.HandlerFunc`，仅由仓库内的标准 HTTP 与 Gin 示例共用。Gin 示例使用 `gin.WrapH` 包装它，并沿用相同的目录和 Provider。该 internal 包不是公共 API，外部业务项目不能直接导入；请参考 `handler.go` 实现自己的请求提取、引擎调用与响应写出。

以下片段用于仓库示例，`nethttp` 指上述示例内部包：

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

共享 `/ows` 端点可以不预绑定协议，但请求必须带足以无歧义识别的 SERVICE。不能因为版本为 1.0.0 就猜测是 WMS 还是 WMTS。

## Fiber v3 示例

Fiber 适配代码和测试位于独立示例模块 `examples/fiber3`。`handler.go` 使用 `package main`，通过未导出的 `ogcHandler` 接入，不作为公共库 API 或独立适配器包发布。业务项目可参考该示例实现自己的接入层；全部 Fiber 依赖留在示例的 `go.mod`。接收原始 RequestURI 时立即拷贝，业务上下文来自 `c.Context()`，不将池化的 `fiber.Ctx` 传入核心。

Fiber 的 SendStream 在 Handler 返回后消费 Reader。适配器通过 fasthttp 的 `CloseWithError` 完成真正的 Body 清理，不在 Handler 返回时提前 defer Close 或 cancel。锁定版本会同时调用实现了两种关闭接口的对象，因此内部异步包装器故意只实现 `CloseWithError`。

测试同时包含真实 TCP 流，以及显式检查“Handler 已返回后才发生 Read”的用例。升级 Fiber 或 fasthttp 后必须重跑这两类测试。

## 接入新的框架

新适配器只完成四件事：提取属于本次请求的原始输入；调用 `Engine.Handle`；写出响应 Plan；在所有结束路径调用 `Plan.Finish`。不要复制参数解析、版本协商或协议异常编码。

| 输入/输出 | 要求                                                    |
| --------- | ------------------------------------------------------- |
| Query     | 原始字符串，保留重复字段和空值，不使用框架单值 Map 代替 |
| Path      | 保留原始转义语义，路径段最多解码一次，`+` 不能当作空格  |
| Context   | 标准 Context，生命周期覆盖业务回调和后续传输            |
| Scope     | 由宿主决定，不采信同名查询参数                          |
| Headers   | 只输出引擎生成的受控字段                                |
| Body      | 不整体缓冲，发送结束/失败/HEAD 都完成一次资源释放       |

新框架应复用 `adaptertest.Run(t, factory)`。工厂必须启动真实 loopback TCP 监听，并注册关闭清理；不能只用可能把响应整体缓冲的框架测试方法证明流语义。

## 生命周期差异

标准 HTTP Handler 内同步执行复制。读取或写入在响应头提交后失败时，使用 `http.ErrAbortHandler` 中止传输，不追加异常 XML。

Fiber 的最强可靠信号是该版本提供的流读取/写入及关闭回调。finish 的字节统计不包括协议头，也不是 TCP 确认量；底层最终缓冲刷新可能比流关闭更晚，不应据此宣称“用户已成功下载”。

HEAD 可配置启用。它仍可能调用普通 Provider 获取内容，但不输出 Body；当前不提供一个单独的低成本 HeadProvider。

## 中间件和异常边界

认证、CORS、连接资源限制、请求追踪和业务限流由宿主中间件提供。不要在下游已经写入响应后，再用统一 JSON ErrorHandler 覆盖协议输出。

Provider 和 Observer 的 panic 不作为正常业务错误使用；宿主可以设置恢复中间件，但不得把中途传输失败当作可重新写 XML 的场景。支持 Context 的 Reader 和合理连接超时仍是阻塞读取的前提。

Echo、Chi 以及其他支持 `http.Handler` 的框架可参考标准 HTTP 示例编写宿主接入层；本次未对每个框架分别运行测试，也没有提供公共适配包。
