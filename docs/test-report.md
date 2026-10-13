# 本地验证记录

本页记录 2026-10-13 的本地实现验证。执行命令为 `./scripts/check.sh --consumers`，26 个检查组通过。机器结果、命令和源码摘要见 [test-results.json](test-results.json)。

## 环境与版本标识

| 项目            | 记录                                                               |
| --------------- | ------------------------------------------------------------------ |
| Go              | `go version go1.27.0 darwin/arm64`                                 |
| 平台            | `macOS-15.6.1-arm64-arm-64bit-Mach-O`                              |
| 开始时间（UTC） | `2026-10-13T02:40:37.964533+00:00`                                 |
| 完成时间（UTC） | `2026-10-13T02:41:21.531921+00:00`                                 |
| 源码 SHA-256    | `c20e0218a83663b0c11f7d75ae665b7b176f292cd67a26bdc400a82e9f001ae5` |

源码摘要按机器记录中的文件清单计算，覆盖自有 Go 源码、模块与工作区清单、示例 PNG。日志与源码标识共同定位本次结果。

## 模块结果

| 模块              | 通过测试事件数 | Staticcheck | go vet | race | CGO_ENABLED=0 构建 |
| ----------------- | -------------: | ----------- | ------ | ---- | ------------------ |
| `.`               |            137 | 通过        | 通过   | 通过 | 通过               |
| `examples/fiber3` |             37 | 通过        | 通过   | 通过 | 通过               |
| `examples/gin`    |             36 | 通过        | 通过   | 通过 | 通过               |

事件数统计顶层测试和子测试的完成事件。三个模块执行相同检查流程，HTTP 与框架示例使用真实 loopback TCP 服务验证内容和状态。

## 验证内容

| 类别         | 已执行内容                                                                   |
| ------------ | ---------------------------------------------------------------------------- |
| 请求与目录   | WMS 四版本、WMTS KVP/REST、矩阵与图层范围、维度和像素                        |
| 流与生命周期 | 字节、已知/动态长度流、HEAD、超时、断连、一次关闭和 Observer 异常            |
| 规范与格式   | 128 项公共声明 GoDoc、goimports、Staticcheck、Markdown/JSON/YAML/Python 格式 |
| XML          | 14 份普通能力、维度能力和异常夹具，按对应 DTD/XSD 或良构性检查               |
| 独立消费     | 临时文件模块代理下的核心引用、模块图与示例 internal 访问规则                 |
| 性能         | 固定请求、模板规模、图层限额和流内存微基准                                   |

核心语句覆盖率为 **84.5%（1,439 / 1,703）**。统计采用根测试集对八个实现包的 `-coverpkg` 结果，重复源码块按位置合并；统计对象为被测试执行命中的实现语句。

关键生命周期、固定矩阵和 HTTP 超时用例另以 `-race -count=5` 重复执行，结果通过。

## Fuzz smoke

每组配置 3 秒、2 个 worker，记录如下：

| 目标      | 执行次数 | 结果 |
| --------- | -------: | ---- |
| KVP       |   12,704 | 通过 |
| WMS       |   15,201 | 通过 |
| WMTS KVP  |   64,824 | 通过 |
| WMTS REST |   26,142 | 通过 |

## XML 来源

XML 校验采用本地 Schema/DTD 和 `xmllint --nonet`，文件来源及摘要见 [schemas.lock.json](schemas.lock.json)。WMS 1.0.0 异常采用良构性验证，其他夹具采用对应版本的 DTD/XSD。

上游 GML/SMIL 的重复命名空间导入诊断保存在原始日志中，目标文档验证结果均为通过。

## 复验命令

```bash
./scripts/check.sh --consumers

go test -race -count=5 \
  -run 'TestReview|TestError|TestFinish|TestFixed|TestDimensionReserved|TestRESTCandidate' \
  ./server ./wmts ./examples/internal/nethttp

go test -run '^$' -bench '^BenchmarkReview' \
  -benchmem -benchtime=300ms -count=3 ./server
```

微基准的环境、输入和结果见 [行为与回归记录](go-review-fixes.md)。这些测量描述固定夹具在本地进程中的耗时和内存分配。

## 结果文件

`docs/test-results.json` 保存本次完整结果，`docs/go-review-results.json` 保存审查与关闭记录。新一次运行将日志写到 `.artifacts/check/`，历史验证副本位于 `.artifacts/go-review-fixes-20261013T022543Z/`。

测试服务接入官方套件的操作流程见 [CITE 接入指南](cite.md)。
