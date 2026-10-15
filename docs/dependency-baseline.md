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

本地测试记录采用 Go 1.27.0、macOS ARM64 环境。CI 文件配置了 Ubuntu 上的 Go 1.25.x / 1.27.x 检查矩阵，定义见 [check.yml](../.github/workflows/check.yml)。

## 固定依赖与开发工具

| 组件        | 版本或入口                                                 | 职责                         |
| ----------- | ---------------------------------------------------------- | ---------------------------- |
| Fiber       | `github.com/gofiber/fiber/v3 v3.5.0`                       | Fiber 示例                   |
| fasthttp    | `github.com/valyala/fasthttp v1.73.0`                      | Fiber 底层 HTTP 与流生命周期 |
| Gin         | `github.com/gin-gonic/gin v1.12.0`                         | Gin 示例                     |
| goimports   | `golang.org/x/tools v0.44.1-0.20260420230617-19499e7caabc` | Go 源码格式与导入分组        |
| Staticcheck | `honnef.co/go/tools v0.8.0`                                | Go 静态分析                  |
| Ruff        | 0.9.10                                                     | Python 脚本格式              |
| Python 3    | `scripts/`                                                 | 检查编排、XML 与兼容表校验   |
| xmllint     | 系统工具                                                   | 本地 DTD/XSD 校验            |

传递依赖和校验和记录在对应的 `go.mod` / `go.sum` 中。

## 环境准备

准备 Go、Python 3、Make 和 xmllint。Python 格式工具与 Go 分析工具按下列命令安装：

```bash
python3 -m venv .artifacts/tools-venv
.artifacts/tools-venv/bin/pip install ruff==0.9.10
export PATH="$PWD/.artifacts/tools-venv/bin:$PATH"

make tools
```

`make tools` 使用 Go 1.27.0 构建 goimports 和 Staticcheck，安装到 `.artifacts/tools`。工具构建通过 `GOTOOLCHAIN` 选择独立分析工具链。

`scripts/check.py` 运行 Staticcheck 时同样固定 `GOTOOLCHAIN=go1.27.0`，避免较新 Go 补丁版本的导出格式超出固定分析工具的支持范围。测试、race、vet、fuzz 和构建继续使用调用环境的 Go 版本；检查报告分别记录测试版本与分析工具链。

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
