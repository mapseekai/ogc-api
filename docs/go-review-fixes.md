# 行为与回归记录

代码定位已随同名测试整理更新；历史测试日志保留运行时的文件名与行号。

2026-10-13 的 R1–R8 复核将八项实现行为与对应测试建立关联，全部完成修复与复验。机器记录见 [go-review-results.json](go-review-results.json)。

## 已验证行为

| 编号 | 实现行为                                                | 主要验证入口                                                                                                           |
| ---- | ------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| R1   | 错误 XML 在父请求下使用独立 1 秒读取窗口，保留父取消    | `TestReviewTimeoutExceptionRemainsReadable`、`TestErrorResponseHonorsParentCancellation`、共享 `business timeout` 用例 |
| R2   | 引擎持有内容时负责异常清理，交付后由 Plan.Finish 完成   | `TestReviewObserverPanicReleasesTransferredBody`、`TestFinishObserverPanicStillCancelsAndCloses`                       |
| R3   | 资源宣告使用完整目录，核对矩阵、图层 Limits、行列与像素 | `TestFixedResourcesUseCatalogMatrixLimits`、`TestReviewUnknownFixedMatrixIsNotAdvertised`                              |
| R4   | 目录与 REST 共用维度保留字规则                          | `TestDimensionReservedNamesAgreeAcrossCatalogAndREST`                                                                  |
| R5   | 原始列表在展开前检查数量，保留转义与空项语义            | `TestListLimitPreservesWireSemantics`、`TestOversizedListsStopBeforeFurtherMapWork`                                    |
| R6   | 完整路径命中后创建字段 Map，使用前后缀匹配占位符        | `TestRESTCandidateCountDoesNotIncreaseAllocations`、`FuzzRESTMatch`                                                    |
| R7   | 公共 API 描述默认值、状态修改、并发和所有权             | `scripts/check_go.go`                                                                                            |
| R8   | 源码、导入、GoDoc 和静态分析进入统一检查流程            | `scripts/format.py`、全部模块 Staticcheck                                                                              |

## 生命周期

正常内容沿用业务期限，错误文档继承父请求并获得独立读取窗口。宿主维护连接写期限，Reader 通过 Context 配合取消。

外部钩子执行前已建立资源清理责任。引擎通过返回 Plan 交付内容，接入层在发送完成或异常时调用 `Finish`。完成回调记录协议状态、处理结果和服务端读取字节数。

标准 HTTP、Fiber 和 Gin 的共享 TCP 用例核对 WMS/WMTS 超时响应的 XML 与内容长度，并覆盖延迟读取、客户端断开和一次关闭。

## 目录与接口

`Router.Resources(layerID, catalog, baseURL, featureInfo)` 使用完整的 `wmts.Catalog` 作为第二个参数。调用方传入已验证目录，以便核对固定矩阵和图层 Limits。这个签名相较初始的 `Layer` 参数增加了完整目录信息，仓库中的调用点采用当前形式。

矩阵位置和像素边界通过共享函数校验，维度保留字使用统一规则。枚举维度按目录处理，自定义维度验证在请求阶段执行。

## 微基准

环境为 Go 1.27.0 / macOS ARM64 / Apple M4。以下为固定输入、`-benchtime=300ms -count=3` 的三次中位数，来源为本轮修复记录。

| 场景                           |  ns/op |   B/op | allocs/op |
| ------------------------------ | -----: | -----: | --------: |
| REST，1 个模板                 |  1,445 |  3,211 |        36 |
| REST，16 个模板                |  1,506 |  3,212 |        36 |
| REST，256 个模板，匹配最后一个 |  2,712 |  3,212 |        36 |
| WMS，1 个图层                  |  1,745 |  2,520 |        38 |
| WMS，65 个图层的限额检查       |  1,664 |  2,584 |        35 |
| WMS，30,000 个图层的限额检查   | 72,028 | 67,864 |        35 |
| 固定 GetTile 引擎链路          |  1,959 |  3,063 |        45 |

REST 基准匹配最后一个模板；图层限额基准采用默认 64 图层上限；引擎基准使用固定内容夹具和 `io.Discard`。表中指标描述这些本地输入对应的处理耗时与分配。

```bash
go test -run '^$' -bench '^BenchmarkReview' \
  -benchmem -benchtime=300ms -count=3 ./server
```

## 工程验证

完整检查包含三个 Go 模块的 Staticcheck、go vet、测试、race 与 `CGO_ENABLED=0` 构建，以及 GoDoc、格式、四组 fuzz smoke、14 份 XML、基准和独立消费者检查。

公共声明注释检查为 128/128，核心语句覆盖率为 84.5%。关键生命周期、目录和 HTTP 用例另执行 `-race -count=5`。

完整结果、源码摘要和执行时间见 [test-results.json](test-results.json)。本地历史日志保存在 `.artifacts/go-review-fixes-20261013T022543Z/`；当前检查日志输出到 `.artifacts/check/`。
