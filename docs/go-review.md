# ogc-api：Go 专项代码审查

> 状态更新（2026-10-13）：上轮 R1–R8 全部已修复并通过本轮 26 组检查。以下保留历史审查依据，当前结果见 [修复与回归记录](go-review-fixes.md)；不要将历史“未修复”描述当作当前状态。

日期：2026-10-13。对象：`/Users/zhang/code/ogc-api` 当前未提交工作区。标准 HTTP 和 Fiber 适配迁入示例之后进行本次审查。

## 1. 结论

**架构方向正确，不建议推倒重来；但当前不能仅凭既有 21 个检查组通过，就视为生产发布验收完成。** 本次复现 2 项应优先修复的生命周期问题，另有目录/REST 校验一致性、资源控制、条件性路由性能和规范改进项。

严重度是本次工程审查的修复优先级，不是 CVSS 评分。P1 建议在真实业务上线前修复；P2 补足一致性与资源边界；R6 只在大量模板部署时优先；P3 是 API 文档与风格整理，不应排在资源泄漏修复之前。

| ID  | 优先级    | 发现                                                    | 证据                                  |
| --- | --------- | ------------------------------------------------------- | ------------------------------------- |
| R1  | P1        | 业务超时阻断已生成的错误 XML                            | 引擎测试 + 真实 TCP 复现              |
| R2  | P1        | Observer panic 漏关内容流；handoff 路径还未取消 Context | 两个阶段的关闭计数与 Context 检查     |
| R3  | P2        | 宣告无法成功请求的固定矩阵 REST 资源                    | Capabilities 与 ValidateTile 对照复现 |
| R4  | P2        | 目录与模板的维度保留字判断不一致                        | 同一个 SECTIONS 维度接受/拒绝对照     |
| R5  | P2        | 图层列表超限前已分配约 1 MB                             | 超限请求基准，3 次实测                |
| R6  | 条件性 P2 | 每个 REST 候选路由都提前复制 Map                        | 1/16/256 模板基准，3 次实测           |
| R7  | P3        | 公共 API GoDoc 契约不完整                               | Go AST 导出声明盘点                   |
| R8  | P3        | 导入分组与注释风格未完全统一                            | goimports 与 staticcheck              |

本轮**仅审查和记录，不修改实现、不自动修复、不提交或推送**。新增复现测试只存在于隔离副本；测试失败代表发现真实问题，并未将它们冒充通过项。

## 2. 范围、方法与本地 skills

已阅读并应用本机 `golang-pro`、`go-concurrency-patterns`，以及 `golang-fullstack-best-practices` 下的 `idiomatic-go`、`concurrency-safety`、`clean-architecture`、`error-handling`；同时沿用本机 Ponytail 与 Ponytail Review 的最小修改、复用和具体案例验证原则。

Go skills 路径根目录：`/Users/zhang/.agents/skills/`。Ponytail 版本为本机缓存的 5.1.1。只使用与协议库相关的语言、并发和架构规则，没有套用数据库、Repository、DDD 或微服务模板。skills 中的绝对化建议仍需结合真实 Go 语义，例如流交付后才取消 Context，不能机械添加 Handler 级 defer cancel。

读取了 38 个自有 Go 文件的实现、测试与调用点。核心路径覆盖 KVP → 版本/请求模型 → 目录快照 → Provider → Payload/Plan → 示例发送，以及 XML、错误编码、REST 模板和跨模块开发脚本。

```text
环境：Go 1.27.0 / Darwin arm64 / Apple M4
Go 源文件数：38
源码 SHA-256：479a3431b90b0c01f5ddade6530f2a26e99290d48c93df68c9b8f671fdf638b8
证据目录：.artifacts/go-review-20261013T020133Z
```

源码摘要算法：按相对 POSIX 路径排序，将每个 Go 文件的路径、NUL、内容、NUL 累积做 SHA-256。完整逐文件摘要在证据目录的 `source-manifest.json`；报告和机器摘要另存 `docs/go-review-results.json`。

## 3. 必须优先处理的问题

### R1 · P1：业务超时后生成的错误 XML 实际发不出去

**位置：** `server/engine.go:21–45、131–145`；`response/plan.go:84–90`。

引擎创建包含业务截止时间的 Context。Provider 等待截止时间后返回错误，引擎将错误编码为响应 Plan，但结束处理时又把这个 Plan 的 Body 包装成使用同一个已过期 Context 的 Reader。第一次 Read 便直接返回 `context deadline exceeded`，因此预生成的 XML 也无法发送。

**具体复现：** WMTS GetTile 回调执行 `<-ctx.Done()` 后返回 `ctx.Err()`。引擎返回状态 500、声明内容长度 297，但读取 Body 得到 0 字节和超时错误。标准 HTTP 真实 TCP 测试中，客户端收到 EOF，而非完整 OWS 异常文档。不是在已经发送图片后要求补发 XML，而是错误发生在首字节发送之前。

**影响：** 客户端无法区分受控业务超时与连接故障；协议异常契约和监控中的“已生成错误响应”与实际接收不一致。

**最小修复方向：** 将业务流的截止时间与限长、已编码错误文档的发送条件区分。业务期限耗尽但父请求仍可写时，用独立、短且有界的错误发送路径；父请求已取消或客户端已断开则停止。正常大内容流仍保留原有取消规则。不要用无条件 `context.Background()` 掩盖所有断连与超时。

**证据：** `review-reproductions.log` 中 `TestReviewTimeoutExceptionRemainsReadable`；`review-tcp-timeout.log` 中 `TestReviewBusinessTimeoutReturnsXMLOverTCP`。

Go Context 的取消会传播给派生 Context；继续使用已经结束的同一个 Context 不会恢复可读状态。[S1]

### R2 · P1：Observer panic 使已接管的 Body 失去清理责任人

**位置：** `server/engine.go:34–45、131–145`。

业务成功返回 Payload 后，引擎先调用 `business` Observer，随后才校验 Payload 和构造 Plan。Observer panic 时，现有 defer 看到 `plan == nil`，只取消 Context，没有关闭已经归引擎所有的 Payload。

另一个路径发生在 `handoff` Observer：Plan 已创建、Lifetime 已绑定，但 Observer 在实际返回适配层之前 panic，调用方尚未拿到 Plan，无法执行 Finish；当前 defer 也不清理它。

| panic 阶段 | 复现的 Body Close 次数 | 复现的 Context 状态 |
| ---------- | ---------------------: | ------------------- |
| `business` |              0，应为 1 | 已取消              |
| `handoff`  |              0，应为 1 | 未取消              |

**影响：** 文件、上游连接或其它 Reader 资源无法按约定归还；handoff 路径还可能延长请求上下文和定时器生命周期。宿主恢复了 panic，不等于资源自动释放。

**最小修复方向：** 在调用外部钩子前建立清晰的当前所有权清理。区分“已拿到 Payload”“已构造 Plan”“已真正交付给适配层”；仅在最后一种状态解除引擎清理责任。`start` Observer 也应放到兜底清理注册之后。保持 panic 向宿主传播，不需要全局吞掉 panic，也不要每个钩子各加一套关闭逻辑。

**证据：** `TestReviewObserverPanicReleasesTransferredBody/business` 与 `/handoff`。复现测试在断言后自行清理测试夹具，不将泄漏资源长期留在用户环境。

## 4. 一致性与资源边界问题

### R3 · P2：Capabilities 宣告了固定矩阵不存在的资源 URL

**位置：** `wmts/rest.go:233–267、278–303`；`wmts/capabilities.go:183–194`。

`resourceApplies` 检查固定图层、样式、矩阵集和维度名称，但没有依据完整目录验证固定 `TILEMATRIX`。当前配置可将 `TILEMATRIX` 固定为 `does-not-exist`，能力文档仍输出该模板的 ResourceURL，而按照同一模板访问会被 `Catalog.ValidateTile` 拒绝。

**影响：** 服务的可发现能力与实际请求校验不一致。只要有该类模板配置，客户端从能力文档获得的资源地址永远无法成功。

**最小修复方向：** 发布资源模板时复用当前目录对矩阵、Limits 和固定维度值的判断，而不是只检查名字是否存在于部分 Map。静态目录可以在注册时拒绝，动态目录需要按本次快照决定能否宣告。避免再复制一套独立的矩阵规则。

**证据：** `TestReviewUnknownFixedMatrixIsNotAdvertised`。这是矩阵固定值的实证，不代表已经逐项证明所有其它固定值场景均错误。

### R4 · P2：两份维度保留字清单已经产生行为分歧

**位置：** `wmts/catalog.go:114–129`；`wmts/rest.go:65–73`。

`Catalog.Check` 与 REST `compile` 各有一份保留字列表，后者比前者多出 `ACCEPTVERSIONS`、`SECTIONS`、`ACCEPTFORMATS`、`UPDATESEQUENCE` 等字段。同一 `SECTIONS` 维度通过目录检查，却在声明为 REST 维度时被拒绝。

**最小修复方向：** 明确哪些名称是全局保留、哪些仅在特定操作下保留，并用同一小型规则函数或常量集合驱动两个入口；不要新增注册中心或通用校验框架。无论最后决定允许或禁止，该规则都应可预测，不能静默出现互相矛盾的校验。

**证据：** `TestReviewDimensionPolicyMatchesREST`。这是一致性问题，不把本项目的保留字保护策略冒充所有 OGC 操作通用的标准强制规定。

### R5 · P2：图层数量上限未能阻止拒绝之前的内存放大

**位置：** `kvp/params.go:102–116`；`wms/request.go:238–252`。

先执行 `strings.Split` 展开完整 LAYERS，再检查 MaxLayers。即使已经记录超限错误，后续仍遍历所有图层，并在 `STYLES=` 情况下分配同等长度的默认样式列表。

默认图层数上限为 64。实测一个 **60,113 字节**、包含 30,000 个图层名的请求最终被拒绝，但每次仍分配 **1,034,607 字节**，3 次基准的中位耗时 **412,258 ns**，约 0.412 ms。

这不是已经证明的无界 OOM：总 Query 长度仍有限制。但图层数量限额没有在数组分配前发挥作用，容易在大量非法请求下增加 GC 压力。

**最小修复方向：** 在展开数组前按原始列表分隔规则计数；发现超限立即返回。可为列表解析添加明确数量上限，并保留 `%2C` 作为标识内容、空样式项和既有错误语义。无需关闭所有校验，也无需为每次请求创建复杂 Budget 对象。

**证据：** `BenchmarkReviewLayerLimitAllocations`，完整结果见第 7 节。

## 5. 性能与工程规模

### R6 · 条件性 P2：路由匹配失败也会复制固定字段 Map

**位置：** `wmts/rest.go:192–218`，尤其 `fields := maps.Clone(r.fixed)` 位于字段匹配之前。

当多个模板的路径段数量相同、而请求命中最后一个模板时，每个候选路由都先复制 Map，即使第一个固定路径段就不匹配。实测 1 个模板为 42 次分配、约 3.4 KB；256 个模板为 552 次分配、约 90.9 KB。

**适用范围：** 当前只有少量通用模板时，不构成上线首要阻断项；若按图层、租户或格式展开为数百模板，才值得优先修复。不能用这组测试宣称所有部署路由性能都差。

**最小修复方向：** 先匹配路径，确认候选有效后再建立字段 Map；或至少延迟到确实需要写入捕获值时再分配。只有实测继续表明匹配本身是瓶颈，才考虑按路径段索引；不应直接引入大型路由框架或对象池。

### 其余性能判断

普通 KVP 引擎链路并不重。新增固定 GetTile 业务夹具的引擎基准中位数为约 1.781 微秒、3,063 B/op、45 次分配。它包含参数解释、目录校验、回调和向 io.Discard 读完响应，不包含真实网络、数据库、地图渲染或对象存储，不能外推成 QPS 承诺。

WMS 每次请求会验证、复制并排序版本列表，存在可以预计算的工作，但版本仅四个；未测出其对业务负载构成实质瓶颈前，不应优先于 R1/R2 或输入分配问题。

## 6. Go 规范、架构和内存安全评价

### Go 编码规范

优点包括类型化请求、`context.Context` 显式传入、可独立注册的操作回调、标准库优先和 `errors.As`/`errors.Join` 错误处理。没有为了接口而复制 Provider、增加空实现或套用 Java 风格层次。使用 `b.Loop()` 的现有和新增基准也保持了当前 Go 基准写法。[S5]

### R7 · P3：GoDoc 契约不完整

AST 统计范围是七个公共实现包中的导出类型、函数和方法，排除测试、结构体字段、常量和私有接收者方法。共 **126 项，其中 101 项没有紧贴声明的 doc comment**。这不等于完全没有项目文档；仓库已经有中文指南，但 IDE 和包文档中的接口约定仍不足。

应首先补齐 `server.New`、`Config`、`Handlers`、`wms.Parse`、`wmts.Parse`、`response.Bytes`、`Plan.Finish` 等的错误、所有权、并发和默认值约定。例如 `Catalog.ValidateMap` 会写入 `LayerDimensions`，`ValidateInfo` 还可能选择默认格式；调用方只看名称容易把它误认为无副作用检查。

不要为凑数量给每个简单方法添加重复函数名的空注释。公开 API 的注释应真正解释行为与约束；Go 的官方文档也将声明前注释作为主要包文档入口。[S2]

### 架构组织

运行时代码的依赖方向清楚：

```text
examples 的 HTTP / 框架接入
    → server
        → wms / wmts → kvp → owscore
        → exception → response / owscore / internal/xmlutil
        → response
```

`server` 是协议编排引擎，不是启动端口的 Web 服务器。业务回调不接收框架 Context。`examples/internal/nethttp` 已通过外部消费者测试验证 internal 导入边界，标准 HTTP 和 Gin 共用一份实现，Fiber 保持示例内部函数。[S4]

现阶段不建议引入 DDD、Repository、DI 容器或插件注册机制。真正应调整的是 `Engine.Handle` 的资源所有权结构，以及目录验证与资源宣告共享规则；不是把文件机械切成更多包。

公共辅助类型和函数的数量应在正式版本前逐项确认。尤其需要区分真正对业务方开放的接口与仅为跨包实现共享的辅助函数；不建议为了减少一个导出名又新增一个空包。

### 内存与并发安全

没有在已审查的运行时核心中发现 unsafe 指针、反射注入或自行启动的 goroutine。参数输入和目录快照有复制边界，Snapshot 内部字段不导出；响应流不整体读入内存，关闭使用共享的 once guard。未知长度与零长度分开表达，真实断连与延迟读取均有既有测试。

这些优点不抵消 R1/R2：Go 的垃圾回收不会替应用保证每个外部资源都按契约执行 Close；Context 取消同样需要每个所有权路径完成清理。[S1]

现有 race 回归通过。新增缺陷复现带 `-race` 后仍然因行为断言失败，日志没有 `DATA RACE` 报告。这说明当前复现的是生命周期/行为问题，不能把它们描述成已经查明的数据竞争，也不能据此宣称所有并发调用都绝对安全。race 只能检测实际执行路径中的竞争。[S3]

### R8 · P3：导入分组与注释规范仍有具体不一致

`gofmt` 与文档格式检查通过，但 `goimports -l` 仍列出 7 个文件：

```text
wms/catalog_test.go
wms/request_test.go
wmts/catalog_test.go
wmts/request_test.go
wmts/request.go
kvp/params_test.go
owscore/dimension_test.go
```

例如 `wmts/request.go:3–8` 将标准库 `slices` 与项目包放在同一组。重建后的 Staticcheck 另报告 `wmts/catalog.go:26` 的 `ST1021`：导出类型 MatrixLink 的注释没有以类型名开始。没有将这条文档风格问题提升为运行时安全缺陷。

建议在已有格式脚本中统一导入整理与检查，而不是新增一套平行脚本；GoDoc 按 R7 的实际契约补充。Go 没有强制的行宽限制，不能把任意 100/120 列门槛当作语言规范。[S6]

## 7. 测试、静态检查与性能证据

### 已执行的检查

既有完整检查 21 组通过，源码迁移后的报告见 `test-report.md`。本次另执行集成覆盖率、隔离副本复现、真实 TCP 超时复现、带 race 的缺陷复现和扩展基准。

本机原有 Staticcheck v0.8.0 用 Go 1.26.1 构建，首次运行不能完整解析 Go 1.27 标准库。已在证据目录的 `tools/` 中用当前工具链重建**同一版本**后重新执行：核心仅有一条 ST1021；Fiber 和 Gin 示例无诊断。未覆盖用户的全局工具，也未改项目依赖。Staticcheck 默认按 go.mod 声明的版本选择适用分析规则，本次没有通过随意改 `-go` 参数隐藏工具问题。[S7]

### 覆盖率

通过根目录测试，并对八个实现包指定 `-coverpkg` 汇总，核心语句覆盖率为 **83.1%（1,359 / 1,636）**。重复源代码块按实际覆盖合并，没有把每个测试二进制里的重复块累计为多份语句。此数值不包含合并后的 Fiber/Gin 子模块 coverage profile。

| 实现包             | 汇总语句覆盖率 |
| ------------------ | -------------: |
| `exception`        |          83.1% |
| `internal/xmlutil` |          82.4% |
| `kvp`              |          87.4% |
| `owscore`          |          85.9% |
| `response`         |          85.9% |
| `server`           |          78.9% |
| `wms`              |          83.3% |
| `wmts`             |          83.6% |

覆盖率不等于覆盖所有排列组合：现有 `deadline` 测试覆盖的是成功响应开始后流读取期限到达，并未覆盖 Provider 超时、错误 XML 尚未发送的路径；Observer panic 的接管资源边界同样缺少测试。这解释了为何既有回归与新增失败复现可以同时成立。

### 基准结果

环境：Go 1.27.0、Darwin arm64、Apple M4。每项 `-benchtime=300ms -count=3`，下表取三次结果中位数。REST 测试请求匹配最后一个模板；LayerLimit 测试超过 64 时最终应返回错误。

| 基准                          |   ns/op |      B/op | allocs/op |
| ----------------------------- | ------: | --------: | --------: |
| `RESTRouteScale/1`            |   1,781 |     3,438 |        42 |
| `RESTRouteScale/16`           |   2,503 |     8,533 |        72 |
| `RESTRouteScale/256`          |  18,762 |    90,870 |       552 |
| `LayerLimitAllocations/1`     |   1,548 |     2,504 |        38 |
| `LayerLimitAllocations/65`    |   2,458 |     4,968 |        39 |
| `LayerLimitAllocations/30000` | 412,258 | 1,034,607 |        39 |
| `EngineGetTile`               |   1,781 |     3,063 |        45 |

基准只用于定位实现成本，不代表生产负载、长期 GC 曲线、网络吞吐或地图服务 QPS。现有按需 Reader 的 1 MiB/64 MiB 流内存基准也不能替代真实业务 Provider 压测。

### 复现入口

新增测试位于隔离副本，正确行为断言当前会失败：

```bash
cd /Users/zhang/code/ogc-api/.artifacts/go-review-20261013T020133Z/worktree

go test -count=1 -run='^TestReview' -v ./server

go test -count=1 -run='^TestReview' -v ./examples/internal/nethttp

go test -race -count=1 -run='^TestReview' -v ./server

go test -run='^$' -bench='^BenchmarkReview' -benchmem -benchtime=300ms -count=3 ./server
```

证据包括 `review-reproductions.log`、`review-tcp-timeout.log`、`review-race.log`、`review-benchmarks.log`、`core-integration-coverage.out`、`api-inventory.json`、`goimports-check.log` 及 Staticcheck JSON 诊断。修复后应把相关用例整理进正式测试，不能一直只放在忽略目录。

## 8. 建议修复顺序与未检查项

先修 R1/R2，补齐业务超时之前/之后、Observer 各阶段 panic 的资源清理测试。接着统一 R3/R4 的目录与模板验证，再处理 R5。R6 按实际模板数量决定优先级；最后整理 R7/R8，并把关键契约加入公开 API 文档。

不建议在这些修复中引入地图引擎、框架适配包、缓存存储或大型抽象。保持当前框架无关核心和示例隔离结构即可。

未执行完整 OGC CITE、真实渲染结果联合校验、长时间 fuzz/压力测试、其他操作系统和 Go 工具链、远程 GitHub Actions。未发现的新缺陷不代表不存在。用户提供的 Context/Observer/DimensionValidator 闭包仍需遵守并发和资源约定。

## 9. 官方依据

本报告中的项目发现均以本机源码和实测为直接证据；下列官方材料用于核对 Go 语义与工具规则，不作为替代源码的抽象评分标准。

- [S1] Go context 文档：`https://pkg.go.dev/context`。
- [S2] Go Doc Comments：`https://go.dev/doc/comment`。
- [S3] Go Data Race Detector：`https://go.dev/doc/articles/race_detector`。
- [S4] Go command / internal directories：`https://pkg.go.dev/cmd/go#hdr-Internal_Directories`。
- [S5] Go testing.B.Loop：`https://go.dev/blog/testing-b-loop`。
- [S6] Effective Go / formatting：`https://go.dev/doc/effective_go`。
- [S7] Staticcheck command-line reference：`https://staticcheck.dev/docs/running-staticcheck/cli/`。
