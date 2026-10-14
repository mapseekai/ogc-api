# ogc-api 修复与五轮深度复核报告

代码定位已随同名测试整理更新；历史测试日志保留运行时的文件名与行号。

日期：2026-10-14。工作目录：`/Users/zhang/code/ogc-api`。

## 结论与范围

上次审查的 R1—R8 均已修复，原始失败反例已转为通过的回归测试。复核又补齐了要素失败页清理诊断、Fiber Reader panic 完成诊断、结果编码语义、UTF-8/自定义编码器边界和整数精度保留等关联路径。

保留协议层、外部能力和示例宿主的分离。未引入第三方运行依赖、插件注册中心、依赖注入容器、兼容包装或后台读取 goroutine。工作区原有未提交内容先做快照再修改；本次未提交、推送或部署。

审查基线为 `3fc08c9015dade0e6f143763802efc5c37f53ddc` 加原有工作区。最终运行代码指纹：`dfdd6b48dcb2d559240a2963cdc509f366c8368e84f43bb3eb93de63d85f68b4`。

报告生成前共有 228 个源码/文档/夹具文件，109 个 Go 文件、14,230 行 Go；其中核心运行代码 50 文件、18 包，根模块及 Fiber/Gin 两个示例子模块均纳入验证。相对修复前快照，47 个文件新增或修改，报告文件另计。

## 已修复问题

| 编号 | 根因与处理 | 主要位置 |
|---|---|---|
| R1 | 大字段在主编码前预算检查；计算 JSON 转义、链接和 base64 展开，目录/状态/document 共用有界编码；Feature 热路径使用类型明确的计量 | `features/stream.go`、`internal/jsonutil/encode.go`、`api/catalog.go`、`processes/response.go` |
| R2 | 单个 Close panic 后仍尝试关闭所有输出，完成后传播首个异常；普通关闭错误合并保存 | `processes/types.go`、`processes/types_test.go`、`processes/response_test.go` |
| R3 | Dismiss 返回值必须保持之前已验证的 JobID、ProcessID 和 dismissed 状态 | `api/processes.go` |
| R4 | 在存在表示的 GET/HEAD 上，先判断整个 `If-None-Match: *`，不再要求 ETag 存在；授权和资源选择仍先执行 | `api/engine.go`、`api/engine_test.go` |
| R5 | 资源发布与运行期路由统一使用 `common.ValidID`；保留合法 Unicode、转义斜杠和非数字矩阵；原始 ID 在 JSON 复制前验证 | `common/id.go`、`api/catalog.go`、`tiles/types.go` |
| R6 | 显式协议选项严格区分缺失、null、错误类型和非法枚举；业务输入的 null 仍交给业务 Schema | `processes/execute.go` |
| R7 | JSON 原始结果不再误写成 object；binary/multipart 使用二进制 Schema，执行与作业结果均描述带 Link 的 204 | `api/openapi.go`、`api/openapi_capabilities.go`、`api/openapi_capabilities_test.go` |
| R8 | 主程序与测试复用 combinedRouter，实际挂载能力文档宣告的 WMTS REST 绑定 | `examples/ogcapi/main.go`、`examples/ogcapi/main_test.go` |

## 结构、复用与并发调整

`api.NewSnapshot` 从 241 行缩减为 57 行编排入口，矩阵、集合、瓦片、过程和发现文档分阶段准备；没有增加 Builder 或通用注册中心。同步执行和作业结果共用 `processResults`，统一所有权、编码失败和 Accept 协商失败的处理。

删除 OpenAPI 生成中未使用的 axes/fields 收集与排序；移除每个非首要素仅为添加逗号而进行的完整缓冲复制；内部查询不再重复 Clone 不可变过滤字段配置，但公共 Defaults 保留独立副本语义。

两类引擎的零值并发配置统一选择 1024 个完整响应，超限直接拒绝。组合 HTTP 示例使用宿主层 publication 固定一整代路由，原子替换新旧协议组合，并在旧请求完成后发出 drained 信号。跨代共享 Provider 仍由共享所有者管理；不把示例生命周期框架加入公共协议层。

新增 `adaptertest.RunAPI`，在 net/http、Gin、Fiber 的真实 TCP 服务上运行同一套资源发现、Features、Tiles、Maps、Coverages、Processes、Jobs、HEAD/304/204、输入限额和错误协商测试。Fiber 与旧协议共用 sendPlan，并显式处理 Reader panic 后的失败诊断和一次性资源回收。

## 五轮复核

| 轮次 | 关注范围 | 结果与证据 |
|---|---|---|
| 1 | 全库依赖/声明、18 个运行包的能力边界与发布调用链；三模块 vet 和测试 | 通过；`round1.log`、`ast-review.json` |
| 2 | 全运行包分配、读取、关闭、Finish 与错误路径；关键包 race 重复5次和分配基准重复3次 | 通过；补修要素页关闭诊断，`round2.log`、`round2-followup.log` |
| 3 | 全库共享状态、并发、取消、准入、原子快照和路由排空 | 三模块各5次乱序 race；2,000 调用方场景各20次；发布场景10次；全部通过 |
| 4 | 协议解析、OpenAPI 和实际返回一致性；JSON标量/数组/对象/null、二进制、multipart、引用 | 9种结果类型测试和10个独立10秒 fuzz目标通过；共享HTTP契约通过 |
| 5 | Ponytail、命名/组织、重复函数、复杂函数及最终门禁 | 补修Fiber panic诊断并重测；最终32项门禁全部通过 |

每轮以全库文件清单为范围，结合 AST 扫描、适用调用链检查和可执行验证；不是把同一测试命令重复五遍。后续发现的修复均进入最终全量门禁。源码文件级 SHA-256 清单保存在 `file-review-matrix.csv`。

## 可执行验证

| 验证项 | 本次实测 |
|---|---|
| 最终项目门禁 | 32/32 通过，0失败 |
| 根模块测试日志 | 339 个通过事件 |
| Fiber 子模块测试日志 | 63 个通过事件 |
| Gin 子模块测试日志 | 61 个通过事件 |
| 总通过事件 | 463；包括顶层测试、子用例及 fuzz seed，不当作463个独立顶层函数 |
| 核心跨包语句覆盖率 | 84.0%，3,253 / 3,874；同口径上次为82.7% |
| 最低 Go 工具链 | Go1.24.0，GOWORK=off、CGO_ENABLED=0，根模块完整测试通过 |
| 跨平台 | Linux/amd64、Windows/amd64 根模块纯Go交叉构建通过 |
| XML与模块边界 | 固定Schema/DTD验证、独立消费者、示例内部包隔离检查通过 |

Fuzz覆盖 kvp、wms、wmts KVP、wmts REST、common、maps、coverages、features、processes 和有界JSON字符串预算。每目标10秒，另外最终门禁又执行了各3秒回归；这不代表无限输入空间已穷尽。

## 分配与负载测量

| 场景 | 修复前 | 修复后 |
|---|---:|---:|
| 4 MiB ID，1 KiB预算，拒绝路径额外分配 | 8,408,538 B/op | 0 B/op |
| 4 MiB href，1 KiB预算，拒绝路径额外分配 | 4,203,760 B/op | 0 B/op |
| 100要素流每次分配 | 56,677 B/op | 44,145 B/op |
| 100要素流分配次数 | 1,107 allocs/op | 709 allocs/op |

0 B/op 指已经构造输入之后的拒绝路径额外分配，不包含预先分配的4 MiB输入。正常流测量使用固定内存夹具，单次耗时存在噪声，不能推导生产吞吐增益。编码预算限制输入/输出及临时分配量级，不是进程RSS的精确封顶；RawMessage空白计入输入预算。

实际loopback HTTP/1.1测试使用64个worker、2,000次请求、104字节固定内存PNG结果：0失败，P95=2.415 ms，P99=3.147 ms。它不是2,000个真实GIS重任务，也不是2,000条并发网络连接的容量证明。

## 明确的公共行为调整

- `processes.Service.Prepare(maxDescriptionBytes int)` 现在要求正数描述预算，NewSnapshot 自动使用目录预算调用；没有旧签名包装。
- Feature ID 接受 string/json.Number/内建数值；任意自定义编码器、指针及错误UTF-8不进入有界编码路径。
- `server.Config.MaxConcurrent=0` 现在与新API一致，选择1024的有限默认值。
- 非法显式null、空枚举和不可寻址ID在边界拒绝，不再静默归一化。

文档、CHANGELOG和最低Go CI已同步。Go模块清单与修复前一致，没有新增运行依赖。

## 证据及边界

详细日志、AST清单、文件矩阵、完整相对修复前工作区的 `changes.patch` 位于 `.artifacts/fix-review/`。修复前完整快照位于 `/private/tmp/ogc-api-fix-20261014/before/`。机器汇总位于 `docs/fix-review-results.json`。

未执行完整官方OGC CITE认证、真实GIS后端长期负载、多小时内存稳定性测试，或Linux/Windows目标系统实际运行。上述结果是本次代码与测试覆盖范围内的复核结论，不替代生产环境容量和标准认证。
