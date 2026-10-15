# 发布就绪评估 — 2026-10-14

## 结论与源码范围

**历史基线：最新发布结论以 [2026-10-15评估](release-readiness-2026-10-15.md) 为准。** 本文以下保留 CITE 前的技术检查记录；最新实现修复、套件问题和覆盖边界见 [官方测试报告](cite-results-2026-10-15.md)。

当前实现具备准备受限协议范围预发布版本的工程基础，但尚未满足本次评估采用的公开正式版发布条件：仓库没有项目许可证，修复后的候选提交尚未在远端 CI 验证，版本标签和发行说明尚未确定。没有发现本次检查范围内新的阻塞级业务功能故障；这不是全输入空间、标准认证或生产容量证明。

评估基线为 `8a8ff6146747b928a93accab61d1c2c3c250f6ee`，加本次检查工具链修复。原有 `AGENTS.md` 改动保留。本次未修改协议公共契约或运行实现，未提交、推送或发布。

## 失败原因与修复

上次 `test-core` 中的 `TestContracts` 和 `TestAdvertisedRESTBindingsAreMounted` 在 `httptest` 创建监听器时失败：`listen tcp6 [::1]:0: bind: operation not permitted`。失败来自执行沙箱，尚未到业务断言。允许 loopback TCP 后，完整核心测试通过，保留了真实网络测试，没有跳过测试或削弱断言。原始日志在 `.artifacts/test-core-sandbox-failure-20261014.log`。

远端当前提交的 [Check 运行](https://github.com/mapseekai/ogc-api/actions/runs/38576235365) 另有工具兼容故障：Go 1.27.2 作业在 `staticcheck-core` 报 `export data version 5 is greater than maximum supported version 4`；最低 Go 1.24.0 作业通过，Go 1.25 作业随后被矩阵取消。已在本机使用 Go 1.27.2 复现同一错误。

`scripts/check.py` 现在仅对 Staticcheck 的子进程固定 `GOTOOLCHAIN=go1.27.0`，与 `make tools` 的固定分析基线一致，并在报告记录该选择。测试、race、vet、fuzz、构建和独立消费者仍使用调用环境的 Go 版本。未来提升模块语言基线时，需要同步提升分析工具及其工具链。

## 功能核对

| 范围 | 当前实现与验证 | 发布边界 |
| --- | --- | --- |
| WMS | 1.0.0、1.1.0、1.1.1、1.3.0；Capabilities、Map、FeatureInfo；轴序、维度、元数据及版本化异常回归 | GET KVP；地图和查询内容由业务后端提供 |
| WMTS | 1.0.0 KVP/REST；Capabilities、Tile、FeatureInfo；矩阵、Limits、像素和宣告路径回归 | 目录与实际路由需要由宿主一致挂载 |
| Common/API | Landing、Conformance、Collections、OpenAPI、不可变快照、条件请求、作用域隔离 | 生成 OpenAPI 不等于完整 OAS 符合性 |
| Features | 只读 GeoJSON、CRS84、四/六坐标 bbox、datetime、属性筛选、limit 截断、cursor、逐要素流 | 无写操作、CQL、Part 2 CRS；查询正确性由 Reader 履约 |
| Tiles | 集合瓦片、规则二维矩阵、字符串矩阵 ID、矩形 Limits、格式协商及 WMTS 共用后端 | 无可变矩阵宽度与动态重分块 |
| Maps | bbox/scaling 读取、尺寸预算、CRS/BBox 返回元信息及 WMS 桥接 | 部分 profile；实际渲染与重投影由 Renderer 提供 |
| Coverages | 固定 19-087 / 1.0.0rc4 草案读取、轴子集、字段选择及 schema | 草案 profile；不宣称完整正式标准实现 |
| Processes/Jobs | 同步、外部异步、raw/document/multipart/reference、作业读取/列表/结果/撤销 | 持久化、恢复和调度属于外部服务；无回调、完整作业过滤或自动 URL 抓取 |
| HTTP 接入 | net/http、Gin、Fiber 共用真实 TCP 契约，含 HEAD/304/204、协商、错误与资源清理 | HTTP glue 为示例；认证、网络 deadline 和业务授权由宿主配置 |

核对入口包括 `api/` 的分发和能力绑定、各能力包、`response/` 生命周期、`adaptertest.RunAPI`、WMS/WMTS 解析与黄金 XML，以及组合示例的真实 PNG 尺寸和新旧协议后端复用测试。

Features 的只读核心和超大 limit 截断语义与 [OGC Features 1.0.1](https://docs.ogc.org/is/17-069r4/17-069r4.html) 的对应范围核对；未逐项执行其完整抽象测试套件。各 profile 的完整边界见 [compatibility.md](compatibility.md) 和 [ogc-api.md](ogc-api.md)。

## 当前验证证据

完整检查命令为 `GOTOOLCHAIN=<版本> ./scripts/check.sh --consumers`，包含格式、Go 风格、兼容表、三个模块的 Staticcheck/vet/test/race/纯 Go 构建、十个 fuzz smoke、XML、独立核心测试、基准和独立消费者。

| 测试工具链 | 结果 | Staticcheck 工具链 |
| --- | --- | --- |
| Go 1.27.2 / macOS ARM64 | 32/32 检查通过 | Go 1.27.0 |
| Go 1.25.0 / macOS ARM64 | 32/32 检查通过 | Go 1.27.0 |

补充验证包括最低 Go 1.24.0 的 `GOWORK=off CGO_ENABLED=0 go test -count=1 ./...`、Linux/amd64 与 Windows/amd64 的纯 Go 根模块交叉构建，以及采用现有 `scripts/tidy.py` 逻辑的临时 modfile 比较。三个模块的 `go.mod`/`go.sum` 无需调整。

18 个核心实现包通过根模块测试集的 `-coverpkg` 统计获得 **83.8%** 跨包语句覆盖率。覆盖率不代表未命中分支没有风险，也不替代协议符合性验证。测试事件数包括子用例和 fuzz seed，不等同于独立测试函数数。

当前日志和机器汇总位于 `.artifacts/release-readiness-20261014/`，`results.json` 记录结果及源码/测试文件 SHA-256，各版本子目录保留对应32项原始日志；Go 1.27.2 的原始完整输出为 `.artifacts/full-check-go1.27.2-20261014.log`，Go 1.25.0 为 `.artifacts/full-check-go1.25.0-20261014.log`。

## 发布前需要完成

1. **明确项目许可证。** 当前 Git 跟踪文件中没有项目级 LICENSE/COPYING/NOTICE，GitHub `licenseInfo` 为 null。由维护者选择项目许可证，并核对随源码分发的官方 schema/DTD 和参考内容的现有授权标记；本次没有替维护者选择许可证。
2. **验证最终候选提交。** 本地修复后的检查通过不改变远端已有失败状态。提交并推送候选版本后，需要远端矩阵通过，再将版本标签指向该提交。
3. **确定版本与发行说明。** 远端尚无版本标签或 GitHub Release。首版可采用 `v0.x` 预发布；发行说明必须公开上述 profile 范围和 CHANGELOG 中的破坏性 API 调整。独立发布示例模块时，还要把当前工作区使用的核心 `v0.0.0` 引用改为真实发行版本。

本报告初次生成时官方 CITE 尚未执行；随后已执行多个正式与开发套件，仍有失败，见 [最新官方测试记录](cite-results-2026-10-15.md)。真实 GIS 后端长期负载、多小时内存稳定性和 Linux/Windows 目标系统实际运行仍未执行；本地 2,000 请求/64 worker 的内存 PNG 场景不能推导生产 GIS 容量。
