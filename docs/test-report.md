# 实际测试报告

## 当前结论

2026-10-13 完成 Go 审查 R1–R8 修复，`./scripts/check.sh --consumers` 的 **26 个检查组全部通过**。这是修复后的新结果；上一轮的 21 组通过与专项发现记录保留在历史审查目录，不能混用。

相比原检查，新增长期门禁为：GoDoc 1 组、Staticcheck 3 个模块、REST fuzz 1 组。修复详情见 [Go 审查修复记录](go-review-fixes.md)，机器结果见 `test-results.json`。

```text
Go：go version go1.27.0 darwin/arm64
平台：macOS-15.6.1-arm64-arm-64bit-Mach-O
开始（UTC）：2026-10-13T02:40:37.964533+00:00
完成（UTC）：2026-10-13T02:41:21.531921+00:00
源码 SHA-256：c20e0218a83663b0c11f7d75ae665b7b176f292cd67a26bdc400a82e9f001ae5
```

## 模块结果

| 模块              | 通过测试事件数 | Staticcheck | go vet | race | 无 CGO 构建 |
| ----------------- | -------------: | ----------- | ------ | ---- | ----------- |
| `.`               |            137 | 通过        | 通过   | 通过 | 通过        |
| `examples/fiber3` |             37 | 通过        | 通过   | 通过 | 通过        |
| `examples/gin`    |             36 | 通过        | 通过   | 通过 | 通过        |

事件数包括顶层测试和子测试，不当作独立业务场景总数。附加 `-race -count=5` 的核心缺陷复验全部通过。标准 HTTP、Fiber、Gin 均执行实际 loopback TCP 契约测试，包括新加入的 WMS/WMTS 业务超时错误 XML。

## 其他检查

公共声明 GoDoc 128/128；goimports 无差异；Staticcheck 各模块无诊断。GoDoc 门禁正/反夹具自检成功。核心独立消费者模块图无第三方运行时依赖，外部导入示例内部 HTTP glue 被拒绝。

覆盖率为 84.5%（1439/1703），使用根测试集对 8 个实现包执行 `-coverpkg`，重复源码块合并。不将覆盖率等同于完整协议认证。

14 份 XML 通过固定 DTD/XSD 或良构性校验；Schema 解析采用 `--nonet`，保留上游重复命名空间警告。

| fuzz 目标   | 本次执行次数 | 结果 |
| ----------- | -----------: | ---- |
| `kvp`       |        12704 | 通过 |
| `wms`       |        15201 | 通过 |
| `wmts`      |        64824 | 通过 |
| `wmts-rest` |        26142 | 通过 |

Fuzz 每组仅配置 3 秒、2 个 worker，是 smoke 而非长期验证。分配优化和三次微基准中位数见 `go-review-fixes.md`；结果不能换算为真实地图服务器 QPS。

## 原始检查记录

| 检查组                        | 结果 | 日志                                               |
| ----------------------------- | ---- | -------------------------------------------------- |
| `format`                      | 通过 | `.artifacts/check/format.log`                      |
| `api-docs`                    | 通过 | `.artifacts/check/api-docs.log`                    |
| `compatibility`               | 通过 | `.artifacts/check/compatibility.log`               |
| `staticcheck-core`            | 通过 | `.artifacts/check/staticcheck-core.log`            |
| `vet-core`                    | 通过 | `.artifacts/check/vet-core.log`                    |
| `test-core`                   | 通过 | `.artifacts/check/test-core.log`                   |
| `race-core`                   | 通过 | `.artifacts/check/race-core.log`                   |
| `pure-go-core`                | 通过 | `.artifacts/check/pure-go-core.log`                |
| `staticcheck-examples-fiber3` | 通过 | `.artifacts/check/staticcheck-examples-fiber3.log` |
| `vet-examples-fiber3`         | 通过 | `.artifacts/check/vet-examples-fiber3.log`         |
| `test-examples-fiber3`        | 通过 | `.artifacts/check/test-examples-fiber3.log`        |
| `race-examples-fiber3`        | 通过 | `.artifacts/check/race-examples-fiber3.log`        |
| `pure-go-examples-fiber3`     | 通过 | `.artifacts/check/pure-go-examples-fiber3.log`     |
| `staticcheck-examples-gin`    | 通过 | `.artifacts/check/staticcheck-examples-gin.log`    |
| `vet-examples-gin`            | 通过 | `.artifacts/check/vet-examples-gin.log`            |
| `test-examples-gin`           | 通过 | `.artifacts/check/test-examples-gin.log`           |
| `race-examples-gin`           | 通过 | `.artifacts/check/race-examples-gin.log`           |
| `pure-go-examples-gin`        | 通过 | `.artifacts/check/pure-go-examples-gin.log`        |
| `fuzz-kvp`                    | 通过 | `.artifacts/check/fuzz-kvp.log`                    |
| `fuzz-wms`                    | 通过 | `.artifacts/check/fuzz-wms.log`                    |
| `fuzz-wmts`                   | 通过 | `.artifacts/check/fuzz-wmts.log`                   |
| `fuzz-wmts-rest`              | 通过 | `.artifacts/check/fuzz-wmts-rest.log`              |
| `schema`                      | 通过 | `.artifacts/check/schema.log`                      |
| `core-independent`            | 通过 | `.artifacts/check/core-independent.log`            |
| `benchmarks`                  | 通过 | `.artifacts/check/benchmarks.log`                  |
| `consumers`                   | 通过 | `.artifacts/check/consumers.log`                   |

日志目录 `.artifacts/check` 会被后续检查更新；本轮副本与额外证据位于 `.artifacts/go-review-fixes-20261013T022543Z`。源码摘要只覆盖 Go 源码、模块/工作区清单和示例 PNG，不含本报告。

本次未执行完整 OGC CITE、其他 OS/Go 工具链、长期负载、远程 CI。没有 commit、push 或发布。实际宿主的中间件、连接超时和数据源仍需业务集成验证。
