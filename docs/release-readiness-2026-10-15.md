# 发布就绪评估 — 2026-10-15

## 结论

**不满足本次正式发布条件，也未达成用户要求的所有原版官方测试通过。** 最新 Processes 正式套件50通过、0失败；WMTS 仍有2个与规范相冲突的直接失败。Maps/Coverages 已补齐原先发现的实际功能缺口，原版开发定义15/0/9、5/0/0，原有14个失败全部消除；开发套件通过不等于正式认证。

本报告保存提交前的验证基线：`8a8ff6146747b928a93accab61d1c2c3c250f6ee` 加本地改动。后续交付提交及推送状态以 Git 历史为准；没有打标签或发布。逐项失败、测试参数和原始证据见 [官方测试记录](cite-results-2026-10-15.md)。

## 当前功能与验证

| 范围 | 验证结果 | 发行边界 |
| --- | --- | --- |
| Features Part 1 | 官方100通过、0失败 | 只读 GeoJSON；25项未声明 Part 2 CRS 跳过 |
| Tiles | 官方15通过、0失败 | 集合规则矩阵；1项 dataset-tilesets 跳过；不宣称完整 OAS |
| WMS 1.3 / 1.1.1 | 必需组163 / 99通过，0失败 | Basic/Queryable；推荐元数据与时间/高程另计 |
| WMTS | 91通过、2直接失败 | KVP/REST、Tile/FeatureInfo、维度及 Limits；all 别名已通过，剩余为官方问题99的像素异常码冲突；13跳过、9无判定 |
| Processes | 50通过、0失败、4跳过 | JSON/纯文本多输出、真实非法格式输入负例和错误内容协商通过；作业持久化/授权/恢复属于外部服务 |
| Maps | 开发定义15通过、0失败、9跳过 | 数据集/集合地图、集合选择、发现与 CRS 元数据、PNG/JPEG/TIFF、CORS；人工视觉问答未启用，其他可选类未声明 |
| Coverages | 旧开发定义5通过、0失败 | 19-087/1.0.0rc4读取与元数据；单元数据的 CIS JSON、DomainSet/RangeType 另经官方 schema 校验 |
| Common、WMS 1.0/1.1.0 | 本地契约、XML、最低版本检查 | 无独立正式 ETS 的完整通过证据 |

CTL 数字包括汇总节点，不等同于独立规范条款数。不得把套件错误、跳过或 CONTINUE 标成通过。CITE 宿主的内存数据、带有界 pause 的 echo 作业与 PNG 样本不能替代生产 GIS 后端验证。

## 工程检查

最终命令为 `GOTOOLCHAIN=go1.27.2 ./scripts/check.sh --consumers`，覆盖32项格式/布局、三模块 Staticcheck/vet/test/race/纯 Go 构建、fuzz、XML、独立核心、基准和消费者检查。最低 Go 1.24.0 另行运行 `GOWORK=off CGO_ENABLED=0 go test ./...`。

原核心失败来自沙箱无法建立 loopback 监听，未跳过真实 TCP 测试；原 Go 1.27.2 Staticcheck 导出格式不兼容通过固定分析工具链1.27.0解决，测试/构建继续使用调用环境版本。原始问题及历史 Go 1.25、覆盖率和交叉构建记录见 [前次基线](release-readiness-2026-10-14.md)，不把历史数字冒充最新候选结果。

当前 Maps/Coverages、Features/Tiles/Processes 回归与工程检查证据在 `.artifacts/cite-20261015-maps/`；WMS/WMTS 原始证据仍在 `.artifacts/cite-20261015-all/`，机器汇总记录源码、夹具、执行器与结果指纹。完整检查最终状态以 `project-check-report.json` 为准。

## 正式发布前仍需完成

1. 解决两个原版 WMTS 失败。最新正式版1.11与规范的异常码要求冲突，对应官方[未关闭问题99](https://github.com/opengeospatial/ets-wmts10/issues/99)。在官方修正前不能同时满足该断言与规范；本次要求不接受以豁免或关闭能力视为通过。公开其余跳过/无判定；Maps/Coverages 原有14个失败已消除，仍需区分开发定义自动检查、人工视觉断言与正式认证范围。
2. 维护者明确项目许可证。当前没有项目级 LICENSE；官方数据/schema 的既有授权也需随分发范围核对，不能代维护者选择许可证。
3. 最终候选必须核对对应提交的远端 CI。本报告中的本地通过和历史远端记录均不能代替该提交的 CI 结果。
4. 确定版本、标签和发行说明，明确上述协议范围与公共接口调整；独立示例模块需引用真实发行核心版本。

未检查生产 GIS 后端长期负载、持久化作业恢复及目标 Linux/Windows 的实际运行。未申请认证或进行任何发布操作。
