# Go 审查修复与回归记录

## 结论

2026-10-13 在 `/Users/zhang/code/ogc-api` 修复上轮 R1–R8 全部 8 项发现。本轮完整回归 **26 个检查组全部通过**；不存在仍未修复的上述审查项。原始审查报告和首次失败证据保留，不把当时的失败记录改写为通过。

实际使用本机 Ponytail、Go 错误处理、Idiomatic Go 和 Go 测试 skills，实施过程中没有调用其他编码 Agent。库仍不生成地图或瓦片，不重新引入公共 HTTP/Fiber 适配包，也没有新增第三方运行时依赖。

## 逐项关闭

| 编号 | 状态   | 修复                                                                                                  | 主要复验依据                                                                                                                                                                                                                               |
| ---- | ------ | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| R1   | 已修复 | 错误内容在父请求下使用独立 1 秒读取窗口，正常流继续使用业务期限；保留父请求取消。                     | `TestReviewTimeoutExceptionRemainsReadable`；`TestReviewBusinessTimeoutReturnsXMLOverTCP`；`TestErrorResponseHonorsParentCancellation`；`TestErrorDocumentHasBoundedIndependentContext`；`shared contract: business timeout/WMS and /WMTS` |
| R2   | 已修复 | 任何外部钩子之前建立资源清理责任；未成功移交的 Payload/Plan 关闭并取消 Context，原 panic 继续传播。   | `TestReviewObserverPanicReleasesTransferredBody`；`TestFinishObserverPanicStillCancelsAndCloses`；`TestStartObserverPanicDoesNotInvokeProvider`                                                                                            |
| R3   | 已修复 | Resources 接收完整 Catalog，共用矩阵可用范围、图层 Limits、行列及像素边界；过滤不可能成功的固定资源。 | `TestReviewUnknownFixedMatrixIsNotAdvertised`；`TestFixedResourcesUseCatalogMatrixLimits`；`TestFixedMatrixRemovedFromLayerLimitsIsNotAdvertised`；`TestResourceMetadataDoesNotAdvertiseUnavailableFixedStyle`                             |
| R4   | 已修复 | 目录与 REST 编译使用同一个 reservedParameter 判断，包含 Capabilities 参数并折叠 ASCII 名称。          | `TestReviewDimensionPolicyMatchesREST`；`TestDimensionReservedNamesAgreeAcrossCatalogAndREST`                                                                                                                                              |
| R5   | 已修复 | ListLimit 先按原始逗号计数再分配，WMS 超限立即返回；BBOX 也使用有界拆分。                             | `TestListLimitPreservesWireSemantics`；`TestOversizedListsStopBeforeFurtherMapWork`；`BenchmarkReviewLayerLimitAllocations`                                                                                                                |
| R6   | 已修复 | 完整候选路径匹配后才复制字段 Map，单段占位符以固定前后缀匹配，无正则捕获分配。                        | `TestRESTCandidateCountDoesNotIncreaseAllocations`；`FuzzRESTMatch`；`BenchmarkReviewRESTRouteScale`                                                                                                                                       |
| R7   | 已修复 | 128 项导出类型、函数和方法具备 GoDoc，注明默认值、输入修改、并发与所有权；新增 AST 注释检查。         | `go run scripts/check_api_docs.go`；`api-doc-guard-selftest.log`                                                                                                                                                                           |
| R8   | 已修复 | 完整 Go 源码使用 goimports，全部模块接入固定 Staticcheck；工具隔离在 .artifacts/tools。               | `format gate`；`staticcheck-core`；`staticcheck-examples-fiber3`；`staticcheck-examples-gin`                                                                                                                                               |

## 生命周期保证与边界

业务超时不再导致尚未发送的错误 XML 在第一次读取时失败。错误文档读取最多使用父请求下的 1 秒独立 Context；正常业务流仍服从原有期限，父请求取消不能被绕过。新增共享真实 TCP 用例在标准 HTTP、Fiber 和 Gin 上验证 WMS/WMTS 业务超时均获得完整 XML 和正确长度。

错误文档的 1 秒 Context 是内容读取窗口，不等于可以强制中断任意网络写操作。接入服务仍应配置 WriteTimeout，并使用支持取消的 Reader。

Observer 的 start/business/handoff/finish 异常路径已覆盖。未返回 Plan 时，引擎关闭未移交资源并取消 Context；不在清理时再次调用 Observer。正常返回之后，仍由接入层调用 Finish。panic 不会被静默吞掉。

## 目录与 REST

Capabilities 不再宣告目录不存在或图层 Limits 不允许的固定矩阵；固定行列、像素和枚举维度也参与过滤。目录和 REST 共用保留字，不再出现目录接受 SECTIONS 维度但 REST 拒绝的分裂行为。

自定义维度验证函数可能有业务开销、依赖权限或时间，因此仍只在真实请求中执行；元数据生成不会主动执行它。固定模板过滤不替代业务的实时数据可用性判断。

**未发布 API 变化：** `wmts.Router.Resources(layerID, catalog, baseURL, featureInfo)` 的第二个参数是完整 `wmts.Catalog`，不再是 `wmts.Layer`。已更新仓库内所有调用；未保留不能校验矩阵的旧签名包装层。

## 性能与分配

环境：go version go1.27.0 darwin/arm64，Apple M4。前值来自上一轮同机审查，后值使用相同基准、固定夹具与 `-benchtime=300ms -count=3` 运行三次取中位数。

| 场景                                         | ns/op：前 → 后 |       B/op：前 → 后 | allocs/op：前 → 后 |
| -------------------------------------------- | -------------: | ------------------: | -----------------: |
| `BenchmarkReviewRESTRouteScale/1`            |    1781 → 1445 |         3438 → 3211 |            42 → 36 |
| `BenchmarkReviewRESTRouteScale/16`           |    2503 → 1506 |         8533 → 3212 |            72 → 36 |
| `BenchmarkReviewRESTRouteScale/256`          |   18762 → 2712 |        90870 → 3212 |           552 → 36 |
| `BenchmarkReviewLayerLimitAllocations/1`     |    1548 → 1745 |         2504 → 2520 |            38 → 38 |
| `BenchmarkReviewLayerLimitAllocations/65`    |    2458 → 1664 |         4968 → 2584 |            39 → 35 |
| `BenchmarkReviewLayerLimitAllocations/30000` | 412258 → 72028 | 1.03461e+06 → 67864 |            39 → 35 |
| `BenchmarkReviewEngineGetTile`               |    1781 → 1959 |         3063 → 3063 |            45 → 45 |

30,000 个图层的请求仍被拒绝，但不再先分配完整大列表；原始查询仍需要有界复制和扫描，因此分配不是零。256 个模板的最后命中场景不再为每个无效候选复制 Map，分配次数与候选数量基本脱钩。

这些是微基准，不是生产吞吐。普通路径耗时会波动，不能据此声称所有场景都变快；本轮重点是确认两处实测的分配放大已经消除。没有引入对象池、路由框架或缓存来掩盖问题。

## 工程检查

| 模块              | 通过测试事件数 | Staticcheck | go vet | race | 无 CGO 构建 |
| ----------------- | -------------: | ----------- | ------ | ---- | ----------- |
| `.`               |            137 | 通过        | 通过   | 通过 | 通过        |
| `examples/fiber3` |             37 | 通过        | 通过   | 通过 | 通过        |
| `examples/gin`    |             36 | 通过        | 通过   | 通过 | 通过        |

测试事件数包含顶层测试和子测试，不等于互不重复的业务用例数。新的原始缺陷复现已经进入正式测试目录。额外将核心生命周期、固定矩阵和 HTTP 超时用例以 `-race -count=5` 重复执行，全部通过，没有 DATA RACE 诊断。

公共类型/函数/方法注释为 **128/128**（不含字段、常量、示例与私有类型方法）。AST 门禁同时执行了正、反夹具自检，缺失注释确实被拒绝。goimports 检查无待整理文件，各模块 Staticcheck 无诊断。

核心语句覆盖率 **84.5%（1439/1703）**，沿用对 8 个实现包指定 `-coverpkg` 的根测试集口径；同一源码块去重后计数，不合并 Fiber/Gin 的独立 coverage profile，也不等同于业务场景覆盖率。

四组 fuzz smoke（KVP、WMS、WMTS KVP、WMTS REST）和 14 份 XML 验证通过。离线 Schema 校验仍保留上游 GML/SMIL 重复命名空间导入警告，没有将警告隐藏。

## 工具和复用

`make tools` 在 `.artifacts/tools` 构建固定版本 goimports 和 Staticcheck，不替换用户全局工具。`make fmt` 整理完整 Go 文件的导入和格式；文档内 Go 代码块使用 gofmt。`make check` 同时执行 GoDoc 与各模块 Staticcheck。

源码与工作区清单保持核心零第三方运行时依赖。HTTP glue 仍在 `examples/internal/nethttp`，Fiber glue 仍在 `examples/fiber3`。独立消费测试确认外部业务不能导入示例内部 HTTP 代码。

## 证据与复跑

```bash
make tools
make fmt
./scripts/check.sh --consumers
```

当前汇总：`docs/test-results.json`。逐项状态：`docs/go-review-results.json`。原始完整回归日志：`.artifacts/check/`。

本轮专项目录：`.artifacts/go-review-fixes-20261013T022543Z`，包含修复前快照、原始失败测试、完整回归日志、重复 race、覆盖率、三次基准和 GoDoc 门禁自检。

```text
执行开始（UTC）：2026-10-13T02:40:37.964533+00:00
执行完成（UTC）：2026-10-13T02:41:21.531921+00:00
源码摘要 SHA-256：c20e0218a83663b0c11f7d75ae665b7b176f292cd67a26bdc400a82e9f001ae5
```

源码摘要包括自有 Go 源码、Go 模块/工作区清单及示例 PNG；算法和文件清单见机器报告。代码未提交、未推送、未发布。没有启动长期运行的服务或修改已有用户服务。

## 未执行的验证

完整 OGC CITE、真实业务渲染联合验证、其他操作系统及 Go 工具链、长期负载测试和远程 GitHub Actions 尚未执行。本次关闭的是上述 8 项明确发现，不代表对所有运行环境或未来业务实现作无缺陷保证。
