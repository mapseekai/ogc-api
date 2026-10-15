# 开发环境与工具

本页汇总仓库清单中固定的版本、工作区组织和检查入口。核心运行配置来自 `go.mod`，框架版本来自示例模块，开发工具来自 `Makefile`、`scripts/` 和 CI 工作流。

## Go 模块

| 模块                                  | `go` 声明 | 用途                       |
| ------------------------------------- | --------- | -------------------------- |
| 根模块 `github.com/mapseekai/ogc-api` | 1.24.0    | 协议、目录、引擎与响应接口 |
| `examples/fiber3`                     | 1.25.0    | Fiber 接入与测试           |
| `examples/gin`                        | 1.25.0    | Gin 接入与测试             |
| `go.work`                             | 1.25.0    | 三个模块的源码联调         |

核心模块使用 Go 标准库。Fiber/Gin 的依赖分别由示例模块管理，标准 HTTP 示例复用标准库接入实现。

`go.work` 将示例引用的核心 `v0.0.0` 映射到仓库根目录。使用源码工作区运行示例时，Go 读取当前协议实现。

CI 配置 Ubuntu 上的 Go 1.25.x / 1.26.x / 1.27.x 完整检查矩阵，各版本独立完成；根模块另以 Go 1.24.0、`GOWORK=off` 和 `CGO_ENABLED=0` 验证最低版本。定义见 [check.yml](../.github/workflows/check.yml)。

## 固定依赖与开发工具

| 组件 | 版本或入口 | 职责 |
| --- | --- | --- |
| Fiber | `github.com/gofiber/fiber/v3 v3.5.0` | Fiber 示例 |
| fasthttp | `github.com/valyala/fasthttp v1.73.0` | Fiber 底层 HTTP 与流生命周期 |
| Gin | `github.com/gin-gonic/gin v1.12.0` | Gin 示例 |
| goimports | `golang.org/x/tools v0.51.0` | Go 源码格式与导入分组 |
| Staticcheck | `honnef.co/go/tools v0.8.2` | Go 静态分析 |
| Ruff | 0.9.10 | Python 脚本格式 |
| Python 3 | `scripts/` | 检查编排、XML 与兼容表校验 |
| xmllint | 系统工具 | 本地 DTD/XSD 校验 |

传递依赖和校验和记录在对应的 `go.mod` / `go.sum` 中。

## 环境准备

准备 Go、Python 3、Make 和 xmllint。Python 格式工具与 Go 分析工具按下列命令安装：

```bash
python3 -m venv .artifacts/tools-venv
.artifacts/tools-venv/bin/pip install ruff==0.9.10
export PATH="$PWD/.artifacts/tools-venv/bin:$PATH"

make tools
```

`make tools` 使用 Go 1.27.2 构建 goimports 和 Staticcheck，安装到 `.artifacts/tools`。工具构建通过 `GOWORK=off` 隔离工作区，并通过 `GOTOOLCHAIN` 选择构建工具链；两项工具的模块最低要求均为 Go 1.26.0，开发工作区仍保持 Go 1.25.0。

[Staticcheck 2026.2.2（v0.8.2）](https://github.com/dominikh/go-tools/releases/tag/2026.2.2) 支持 Go 1.27.2 的新版导出数据。goimports 使用与该版本 Staticcheck 一致的 x/tools v0.51.0 正式标签。工具源码版本决定其解析能力，构建工具链版本与被分析代码的语言版本分别管理。

`scripts/check.sh` 将参数传给 `scripts/check.py`。Staticcheck、vet、测试、race、fuzz、构建及独立消费者都使用调用环境选择的 Go 版本，不再对 Staticcheck 单独固定旧工具链。Staticcheck 按各模块 `go.mod` 的语言版本分析，检查报告记录实际 Go 版本。CI 同时输出工具二进制的构建信息，并关闭矩阵快速失败，保留各版本的完整结果。

## 格式约定

`make fmt` 使用 goimports 整理 Go 源码及导入，使用 Ruff 整理 Python 脚本，并通过 gofmt 格式化 Markdown 中的 Go 代码块。能力表由 `scripts/compatibility.py` 生成和校验。

Markdown、JSON 和 YAML 的文本样式由 `.editorconfig` 约定：UTF-8、LF 换行、文件末尾换行和行尾空白清理。编辑文档时沿用现有标题层级、表格和代码块结构，编辑器通过 EditorConfig 应用这些文本设置。

测试与源文件同目录同名，场景、回归、模糊测试和基准测试集中在对应 `_test.go`。文件布局和 Go 规则见 [代码规范](code-style.md)。`go run scripts/check_go.go` 检查测试配对、常量及测试声明顺序、辅助函数标记、接收者一致性和公共 GoDoc。goimports 使用项目路径参数，固定标准库、第三方和项目内包三组导入。

## 常用命令

| 命令                             | 执行内容                                    |
| -------------------------------- | ------------------------------------------- |
| `make fmt`                       | 同步能力表，格式化 Go/Python 源码与文档内 Go 示例          |
| `make test`                      | 遍历工作区全部模块运行测试                  |
| `make check`                     | 全部模块的代码、格式、fuzz、XML 与基准检查  |
| `./scripts/check.sh --consumers` | 完整检查并运行独立消费者测试                |
| `make consumers`                 | 用临时模块代理验证外部项目引用              |
| `make schema`                    | 校验版本化 XML 夹具                         |
| `make tidy`                      | 在临时 modfile 中整理各模块依赖，再写回清单 |

示例监听地址通过 `OGC_ADDR` 设置，默认 `127.0.0.1:8080`。格式检查可通过 `GOIMPORTS` 指定工具绝对路径，静态检查可通过 `STATICCHECK` 指定工具绝对路径。

## 测试产物

检查日志和机器汇总位于 `.artifacts/check/`，构建结果位于 `.artifacts/check/build/`。官方 XML Schema 与 DTD 保存在 `testdata/schemas/`，其来源和 SHA-256 由 [schemas.lock.json](schemas.lock.json) 固定。

升级框架后，重点复验请求数据生命周期、延迟流发送、Context 取消和真实 TCP 契约。升级工具后，执行格式与 Staticcheck 检查，并保存对应工具链和命令记录。

## 工具链兼容性验证（2026-10-15）

以下为基于 `4100269` 的本地工作树在 Linux AMD64 上执行的结果。完整检查使用 `GOTOOLCHAIN=local ./scripts/check.sh --consumers`，通过 PATH 分别选择下列工具链，并设置 `GOMAXPROCS=3`、`GOFLAGS=-p=3` 限制并行资源。每个版本的 Staticcheck 使用该版本 Go 加载包。

| Go 版本 | 验证范围 | 结果 |
| --- | --- | --- |
| 1.24.0 | `GOWORK=off CGO_ENABLED=0 go test -count=1 ./...`，独立核心 | 通过 |
| 1.25.14 | 三模块完整检查及独立消费者 | 32/32 通过 |
| 1.26.9 | 三模块完整检查及独立消费者 | 32/32 通过 |
| 1.27.2 | 三模块完整检查及独立消费者 | 32/32 通过 |

完整检查保留格式、Go 规范、兼容表、三模块 Staticcheck/vet/test/race/纯 Go 构建、10 个 fuzz 目标、XML 校验、独立核心、基准和消费者验证。另已从 Go 1.25.14 环境运行 `make tools`，确认其能选择固定的 Go 1.27.2 构建开发工具。

同一核心源码在 Go 1.27.2 下使用 Staticcheck v0.8.0 可复现 `export data version 5 is greater than maximum supported version 4`；使用 v0.8.2 后通过。各版本的机器报告、日志和工具构建信息保存在 `.artifacts/toolchain-validation/`。
