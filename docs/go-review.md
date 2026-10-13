# Go 代码质量检查

本页汇总代码组织、资源生命周期、规范和性能的验证方式。2026-10-13 的 R1–R8 复核记录均已关闭，对应结果见 [go-review-results.json](go-review-results.json) 和 [行为与回归记录](go-review-fixes.md)。

## 检查范围

检查覆盖公共协议包、服务目录、处理引擎、字节与流响应、三种 Web 接入示例、测试及开发脚本。采用本地 Go 编码、并发、错误处理、架构、测试与 Ponytail 复核规则，逐项核对调用关系和实际输入。

| 维度        | 实现行为                                         | 验证方式                            |
| ----------- | ------------------------------------------------ | ----------------------------------- |
| Go 编码规范 | 类型化请求、显式 Context、错误链与公共 API 注释  | GoDoc AST 检查、Staticcheck、go vet |
| 架构组织    | 协议核心、内容回调与宿主接入分工                 | 源码依赖检查、独立消费者测试        |
| 内存与资源  | 有界参数读取、快照复制、Payload 所有权和一次关闭 | 边界测试、关闭计数、流读取测试      |
| 并发与取消  | 目录快照替换、请求独占、父请求取消与协作超时     | race、并发替换及生命周期测试        |
| 风格一致性  | 源码与导入使用 goimports，文档 Go 示例使用 gofmt   | 格式检查与固定工具版本              |
| 性能        | 列表展开前限额、路由命中后分配                   | AllocsPerRun、固定输入微基准        |

## 协议与业务接口

`server.Engine` 使用 `wms`、`wmts`、`kvp`、`owscore` 组织请求。业务通过按操作注册的 `Handlers` 提供内容；HTTP 和框架适配位于 `examples/`。

标准 HTTP 与 Gin 共享示例内部辅助包，Fiber 使用示例内的私有接入函数。独立消费测试核对公共核心导入和示例 internal 访问规则。

## 资源与异常路径

测试对 Provider 返回、Payload 校验、Plan 交付、发送完成、HEAD 和异常中断逐个核对关闭责任。Body 由 `Bytes` / `Stream` 的共享保护管理，宿主通过 Plan 指针执行 `Finish`。

Observer 的 `start`、`business`、`handoff`、`finish` 路径具有专门测试。业务超时后的错误 XML 使用独立读取窗口；父请求取消与正常内容期限分别校验。

目录通过 Snapshot 深拷贝与引用替换共享。每个请求使用单独的类型化对象，维度校验和默认格式补充发生在该对象上。

## 实测记录

| 项目                       | 2026-10-13 记录                         |
| -------------------------- | --------------------------------------- |
| 本地环境                   | Go 1.27.0 / macOS ARM64 / Apple M4      |
| 完整检查                   | 26 个检查组通过                         |
| 公共类型、函数与方法 GoDoc | 128 / 128                               |
| 核心语句覆盖率             | 84.5%，1,439 / 1,703                    |
| 框架契约                   | 标准 HTTP、Fiber、Gin 真实 loopback TCP |
| 附加回归                   | 关键用例 `-race -count=5` 通过          |

GoDoc 统计覆盖七个公共实现包中的导出类型、函数和方法。覆盖率采用根测试集对八个实现包的 `-coverpkg` 结果，按源码块去重合并。

## 复验入口

```bash
./scripts/check.sh --consumers

go test -race -count=5 \
  -run 'TestReview|TestError|TestFinish|TestFixed|TestDimensionReserved|TestRESTCandidate' \
  ./server ./wmts ./examples/internal/nethttp
```

方法依据：[Go Doc Comments](https://go.dev/doc/comment)、[Context](https://pkg.go.dev/context)、[Race Detector](https://go.dev/doc/articles/race_detector)、[Staticcheck](https://staticcheck.dev/docs/)。

机器记录保留初次审查与后续关闭的时间、源码摘要、测试名称和测量数据。完整本地结果见 [测试记录](test-report.md)。
