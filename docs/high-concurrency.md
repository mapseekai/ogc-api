# 并发、资源预算与实现替换

## 请求路径

`api.NewSnapshot` 在发布阶段完成元数据复制、目录校验、矩阵索引、发现文档编码和 ETag 计算。读请求获取一次不可变快照，元数据直接引用已编码的只读字节；普通请求不复制整个目录、不扫描全部图层、不初始化数据源。

`api.AtomicCatalog` 使用原子指针发布新快照。每个快照同时包含公开资源与后端绑定，防止更新中出现新目录配旧 Provider。请求之间不共享可变的 Query、响应 Reader 或 Iterator。

## 并发准入

新 API 与传统 WMS/WMTS 引擎均默认允许 **1024 个完整响应同时在途**，分别通过 `api.Config.MaxConcurrent` 和 `server.Config.MaxConcurrent` 调整。零值选择有限默认值；超过容量立即拒绝，且不建立等待队列。

配额通过原子计数取得。达到上限直接返回 503 和 `Retry-After: 1`，不排队、不为等待者创建 goroutine。配额覆盖目录读取、业务操作和后续内容传输，直到 `Plan.Finish` 才释放。因此慢客户端不会在 Handler 返回后绕过配额。

API Observer 的 `reject` 事件记录准入拒绝；已接纳请求记录 `start`、`business`、`handoff`、`finish`。Observer 同步执行，调用方必须保证并发安全且不阻塞，指标写入可以转交宿主的有界采集机制。

并发上限不是 QPS 承诺，也不限制宿主提前创建的 HTTP 连接或请求体接收。宿主仍须配置连接数、读写超时、请求头限制、入口限流与身份授权。

## 默认内存边界

| 项目 | 默认值 | 生效阶段 |
| --- | ---: | --- |
| 原始 Query | 64 KiB | 分配参数 map 之前 |
| URL Path | 16 KiB | 路由之前 |
| Query 参数数量 | 128 | 解码之前 |
| 参与协议处理的请求头 | 16 KiB | 参数与目录处理之前 |
| 执行 JSON Body | 1 MiB | 读取与解析执行文档 |
| 单个 Feature | 1 MiB | 单要素编码 |
| JSON document 结果 | 8 MiB | 有界结果汇总 |
| 预编码发现文档总量 | 64 MiB | 快照构造 |

这些是每个请求、要素或快照的边界，不是进程总内存预算。总体压力还取决于并发数、后端游标缓冲、渲染图像和旧快照的在途数量。实际部署应根据后端连接池及单请求内存选择 MaxConcurrent。

## 流式传输

Features 按 `Iterator.Next(ctx)` 拉取一个要素，编码后交给 Reader，避免整页 FeatureCollection 驻留。Stream 会校验 Reader 的请求 Limit 和实际输出计数；后续读取失败时中止传输，不补造一个完整的成功文档。

瓦片、覆盖数据、地图和单个原始处理结果使用 `response.Payload`。多输出处理只缓存 multipart 头，通过 `io.MultiReader` 串联各输出；不使用 `io.Pipe`、额外后台 goroutine 或整结果拼接。

必须正确区分读取完成与发往客户端成功。`BytesRead` 是服务端读取并交付传输层的字节数，不代表客户端已确认。宿主应把实际发送错误传入 Finish，并在流失败后终止已发出的响应。

## Context 与生命周期

引擎超时覆盖目录读取、外部操作和正常响应读取。Context 在 `Plan.Finish` 后取消，而不是在 `Handle` 返回时取消。协议错误文档有独立的短读取窗口，仍受父请求取消约束。

Context 无法强行打断一个忽略取消、没有 I/O deadline 的任意 Reader。数据库、对象存储和远程调用必须协作取消。接入层负责 HTTP 连接的真实 deadline；异步作业提交后由外部调度器管理作业生命周期。

完成回调不得重入自己的`Plan.Finish`。Finish 精确关闭一次内容，并执行受保护的内部回收逻辑。宿主修改 `OnFinish`、回调 panic 或外部 Close panic 不会移除内部取消与准入回收。

## 性能验证方法

仓库保留三种互不混淆的证据：

1. **生命周期压力测试**：2,000 个调用方同时持有响应。2048 配额时全部接纳；64 配额时64个接纳，其余立即拒绝，结束后配额恢复。
2. **真实 TCP 契约/负载测试**：64 个有界 HTTP/1.1 客户端处理2,000次固定内存瓦片请求，验证状态、字节内容、读取和关闭。
3. **微基准**：对元数据、瓦片请求以及100个要素的编码测量耗时、B/op 和 allocs/op。

```bash
go test -race -count=5 -run 'TestTwoThousand|TestAtomic|TestInFlight|TestScoped|TestObserver' ./api
go test -race -count=5 -run 'TestAdmission|TestClosePanic' ./server
go test -count=1 -run '^TestPooledHTTPConcurrency$' -v ./examples/ogcapi
go test -run '^$' -bench 'Benchmark(MetadataParallel|TileParallel|CollectionStream)$' \
  -benchmem -cpu=1,4,8 -count=3 -benchtime=300ms ./api ./features
```

`RunParallel` 的 ns/op 是总墙钟时间除以操作数，不是单请求 P95。固定小瓦片的本机 HTTP 吞吐量不是 PostGIS/GDAL/远程服务的生产指标。实际结果与原始日志位置见 [升级验证记录](upgrade-test-report.md)。


## 编码预算与临时内存

要素 ID、链接、JSON 字符串转义和二进制结果的 base64 展开在主编码分配前计入预算。元数据复制、作业状态和 document 结果复用内部预算检查；原始流正文不整体缓冲。为追加逗号而复制整个要素缓冲的路径已移除。

预算限制输入与编码输出的量级，不能代表 Go 堆峰值或进程 RSS 的精确上限。JSON RawMessage 的空白也计入输入预算；错误 UTF-8 和任意自定义 MarshalJSON 实现不进入有界协议编码。数据源自身的缓冲、图像内存、宿主连接和存活旧版本仍由部署方预算。

## 跨协议发布与排空

`api.AtomicCatalog` 只切换资源式 API 快照。组合 HTTP 示例的 `publication` 在宿主层固定整个路由代际，将 `api.Engine` 和 `server.Engine` 一起替换。新请求使用新代，旧请求在原代完成；`publish` 返回的通道关闭后，宿主才能回收旧代独占 Provider。跨代共享 Provider 的生命周期必须由共享所有者管理。

此示例以同步 net/http Handler 返回表示完成，因此保持在 `examples/ogcapi/`。Fiber 延迟流通过 `Plan.Finish` 管理生命周期，不能直接以 Fiber Handler 返回作为资源排空信号。
