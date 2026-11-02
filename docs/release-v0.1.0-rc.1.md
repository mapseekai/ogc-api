# v0.1.0-rc.1 — 核心库技术预览

日期：2026-11-02。状态：候选发布准备，安装与最终验证状态待补充。

## 定位与发布范围

本次只发布 `github.com/mapseekai/ogc-api` 核心 Go 模块的**有限范围技术预览**。它不是正式 `v0.1.0`，不是完整 OGC 服务器实现，也不是 CITE 全通过或 OGC 认证声明。预发布接口仍可能调整；评估方应固定版本、核对 profile，并在自己的 Provider/宿主上验证。

候选准备基于产品提交 `3705849fa84e93fac1eb300f73531ff455289434`，源码树 `828aa6c8060309771385a0442f6757bf7a42c5f9`；这只是发布准备输入，**不是加入发行材料后的最终候选提交，也不是新的 ETS 运行标识**。最终标签指向与验证证据由发布执行记录补齐。

核心负责协议解析、校验、目录/能力声明、类型化后端请求与响应所有权。外部应用负责存储、筛选求值、渲染/重投影、授权、内容编码、缓存和作业执行。库不引入 GIS 引擎、存储 SDK、Redis、压缩器或身份认证系统。

`examples/` 中的 net/http、Gin、Fiber 和演示后端提供接入参考，不作为稳定、可 import 的适配器 SDK 发布；`examples/internal/nethttp` 受 Go internal 规则限制，Fiber handler 为示例私有实现。此次不发布独立示例模块版本，不交付生产部署或消费方迁移。

## 工具链与使用

| 场景 | 最低 Go 版本 | 边界 |
| --- | --- | --- |
| 核心库消费者，独立 module | 1.24.0 | 关闭本仓库工作区影响，验证实际候选模块 |
| 本仓库源码工作区及示例 | 1.25.0 | `go.work` 联调根模块、Fiber 与 Gin 示例 |

标签发布且安装验证完成后，已有 Go module 可固定候选版本：

```bash
GOWORK=off go get github.com/mapseekai/ogc-api@v0.1.0-rc.1
```

此处是目标安装命令，不代表当前标签已经存在、Go module proxy 已可见或安装成功。不要用源码目录的 `replace` 或工作区构建结果代替干净消费者的版本安装验证。

宿主自行将 HTTP 请求转换成 `server.Input` / `api.Input`，并在发送、HEAD、204/304、取消和异常路径调用 `response.Plan.Finish`。借用字符串、流生命周期与授权边界详见[接入文档](adapter-guide.md)和[条件响应契约](conditional-responses.md)。

## 本候选包含的能力

完整版本、绑定与参数边界见[协议能力表](compatibility.md)，这里不将实现清单等同于标准认证：

- WMS 1.0.0/1.1.0/1.1.1/1.3.0 的 GET KVP；WMTS 1.0.0 的 KVP/REST，含目录、矩阵、能力文档与协议异常。
- 资源式 Common、只读 Features、规则矩阵 Tiles、有限 Maps、草案 Coverages 和外部 Processes 能力接口。
- Features 可选 Part 2 多坐标系契约；原生轴序/维度及输出、bbox、过滤 CRS 独立传递，转换由 Reader 实现。
- #1：条件/编码输入、共享弱 ETag 判断及后端明确 Content/NotModified/NoContent；WMS GetMap、WMTS GetTile 和逐 tileset 的 API Tiles 可显式启用 204，默认关闭。API Maps 支持共享 Renderer 的 304，但禁止 204。目录/后端授权和宿主交付前复核不因缓存命中跳过；Vary 合并、取消及关闭失败路径保留。
- #2：可选有限 Features Part 3/CQL2 Text/JSON 请求解析、类型化 AST、queryables 与能力约束；不是 SQL 生成或查询执行引擎，不自动声明全部 Part 3/CQL2 符合性。
- #3：可选要素 offset 扩展及有界集合目录 limit/offset 分页、惰性元数据；页外已授权路由不依赖当前目录页，条件缓存隔离包含 Scope 与表示。

### 兼容注意事项

既有 Reader/Renderer 和传统回调的方法签名保留。新增 Payload 状态零值仍为正常内容；空 bytes 不自动变成 204。新增分页及过滤能力默认关闭。

普通属性过滤的保留名 `offset` 不能继续用于 `FilterProperties`，需调整配置；CQL2 queryable 同名属性不受此限制。offset 与 cursor 互斥，分页链接保留筛选/CRS 参数。其他开发期 API 变化继续见[变更记录](../CHANGELOG.md)，不要将首个候选解释为此前开发快照之间完全无迁移成本。

## 明确不支持与未覆盖范围

- #4 Features CRUD、#5 WFS、#6 WCS 暂不处理，本候选没有这些协议/写入入口；不得把 API Coverages 或 WMS 地图冒充 WCS。
- CQL2 仅文档列出的语法、类型和显式操作符范围；不提供通用函数、算术、数组、任意 CRS 转换或所有过滤符合性类。
- Tiles 不支持可变矩阵宽度或动态重分块；Maps 不实现生产渲染、重采样或全部可选类。
- Coverages 为 `19-087 / 1.0.0rc4` 草案读取 profile，不是最新完整覆盖服务认证。
- Processes 的持久化、调度、恢复与授权由宿主实现；不自动获取任意 reference URL，不实现完整作业过滤及所有可选类。
- 核心不实现 WMS/WMTS XML POST/SOAP；特定时间/高程、生产 GIS 后端长期负载及目标平台实际运行不由本候选自动保证。

## CITE：历史证据，不是当前候选通过

以下为[2026-10-15 版本化报告](cite-results-2026-10-15.md)的历史摘要。执行基线为 `8a8ff6146747b928a93accab61d1c2c3c250f6ee` **加当时本地改动**，日期为 2026-10-14—15。WMS/WMTS 沿用该阶段前一批原始结果，不能把下表称为 2026-11-02 最终候选的复验。

| 套件 | 历史通过 / 直接失败 / 跳过 | 必须保留的范围限制 |
| --- | --- | --- |
| Features ETS 1.9 | 100 / 0 / 25 | 当时 Part 1 Core/GeoJSON；未声明 Part 2 的跳过，非当前 Part 2 复验 |
| Tiles ETS 1.2 | 15 / 0 / 1 | 集合瓦片；dataset-tilesets 未声明 |
| WMS 1.3.0 ETS 1.34 | 163 / 0 / 0 | Basic/Queryable 必需组；旧推荐元数据失败另计 |
| WMS 1.1.1 ETS 1.23 | 99 / 0 / 0 | 已执行通用、Basic、Queryable、严格 bbox 组 |
| Processes ETS 1.3 | 50 / 0 / 4 | 当时声明的输入/输出与同步/异步 profile |
| WMTS ETS 1.11 | 91 / 2 / 13 | 另有 3 个继承失败、9 个 CONTINUE；I/J 异常码冲突未改算通过 |
| Maps 1.10-SNAPSHOT | 15 / 0 / 9 | 官方开发源码定义；不是正式认证 ETS，人工视觉断言未启用 |
| Coverages 0.1-SNAPSHOT | 5 / 0 / 0 | 旧草案开发定义的元数据检查，不覆盖全部读取/子集语义 |

TestNG/CTL 计数口径不同，CTL 包含汇总节点，不能当作独立规范条款数。SKIP、CONTINUE、未执行及人工问答未启用均不是通过。历史记录中的上游 WMTS 问题状态也是当时核验结果，不代表本次重新查询上游状态。

### 2026-10-30—31 Features 历史对照

本次另行恢复 `ogc-features-integrated-review` 历史证据包，读取冻结锁、summary 和原始 TestNG XML，并核对非配置 test-method 的逐项计数。下面是**读取历史报告**的结果，不是 2026-11-02 重跑；也不能将不同产品提交的结果合并为同一候选全通过。

| 执行方式 | 历史 2D 通过/失败/跳过 | 历史 3D 通过/失败/跳过 | 产品与时间 |
| --- | --- | --- | --- |
| 官方 ETS 1.9 普通 classpath / TEAM Engine 6.0.0-RC2 | 249 / 0 / 5 | 87 / 3 / 10 | `cd013ff4909c49b2f203183a4a1bcea849ae86c7`，2026-10-30 23:57 UTC |
| 官方 ETS 1.9 AIO（冻结 v3 对照，v4 明确复用） | 248 / 1 / 5 | 85 / 5 / 10 | `2ca754a0d0f66160a0d080b9c557237f2de593be`，2026-10-31 历史批次 |
| **非官方本地** 1.9-local-crs3d-integrated-r6 | 249 / 0 / 5 | 144 / 0 / 5 | `2ca754a0d0f66160a0d080b9c557237f2de593be`，2026-10-31 02:59 UTC |

官方 1.9 源码基线为 `e7c8a96deff936f7ae9c4cbbafb900ce65371d2c`；本地 r6 源码为 `721215892cc587c87b32c58e69f2f7b735d5af29`。普通 classpath 解决 AIO 资源打包冲突，不代表修复了官方六值 extent/3D 数值断言缺口；官方 3D 仍有 FAIL。r6 修正版与附加正负 HTTP 控制不属于官方发行或认证。本地剩余 2D/3D 各 5 个 SKIP 为固定 bbox 未返回要素，正样本几何交集断言未覆盖，不能改算通过。各变体测试总数不同，不能直接把通过数差值解释为修复的规范条款数。

历史证据包内索引（不是核心模块依赖或本仓库路径）：

- `prior-official-analysis/runs/official-ide-2d/summary.json` 与 `official-ide-3d/summary.json`；原始 XML 会话分别为 `eb6fd716-75bf-43f5-8a63-902ebf168088`、`d78e1424-5499-4531-a00b-1070881d8869`。
- `freeze-v4/integration-lock.json`、`freeze-v4/validation-final.json`：最终产品/ETS/JAR 锁与官方结果复用说明。
- `freeze-v3/evidence/combined-cite-optimized/`：官方 AIO 对照和独立 HTTP 控制，官方有 FAIL 时总 runner 保留非零退出。
- `freeze-v4/evidence/local-r6-actual/summary.json` 及两组 `results/testng/.../testng-results.xml`；本地原始 XML 会话为 `eb7e3602-b4f8-4f06-b66b-1d72180639e2`、`9a9d2c28-b2e5-42b5-8512-1f9f4e7fa264`。

锁中官方 AIO SHA256 为 `cb8c61c156898f42e9a9a0fbf721b3ac0a2aae6345c0ca0b7ed45f31e14faac2`，本地 r6 AIO 为 `133699785490e5d90a5b0e24ca29e46867b00667ab5c8b8f8b4a153b79015a0d`。这些是历史锁定值，不表示本次已重建相同 JAR；包省略清单明确不含重复二进制/工具缓存。复现须重新准备工件、核验锁与源码，并保留原官方不利结果。

这些 Features ETS 1.9 运行覆盖 Part 1/可选 Part 2，**不覆盖 Part 3**。本候选没有 Part 3 官方 ETS 通过证据。上述历史产品均早于候选的 #1/#2/#3 收尾，不能替代最终候选复验；其他协议仍只有前述 10 月 15 日历史记录。复现方法见 [CITE 接入指南](cite.md)及[多坐标系记录](features-crs.md#真实http与官方ets)。

## 候选验证与正式发布门禁

| 验证项 | 本说明编写时的状态 |
| --- | --- |
| 最终候选源码的完整项目检查、最低 Go、race、独立消费者 | 待发布执行记录补齐；历史通过不替代最终候选 |
| `v0.1.0-rc.1` 标签指向及干净消费者安装 | 待验证；上述命令不构成成功记录 |
| 最终候选远端 CI | 待核验准确提交，不引用旧提交结果代替 |
| 最终候选官方 ETS / Part 3 官方 ETS | 未在本次重跑；无当前全通过或认证证据 |

[2026-10-15 正式发布门禁](release-readiness-2026-10-15.md#正式发布前仍需完成)原样保留。此次获准的是限域技术预览，不是正式门禁全通过的判断：原版 WMTS 失败及所有未覆盖项、许可证与分发授权、最终源码 CI、版本/发行说明和示例依赖均需按原门禁处理、留存证据。正式 `v0.1.0` 不能仅把本候选重命名后发布；没有申请或获得 OGC 认证。
