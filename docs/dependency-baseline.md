# 依赖与环境基线

本次实施在用户 Mac mini 的 Darwin/arm64 环境执行，实际 Go 工具链为 `go1.27.0`。版本通过本机模块查询和锁定源码核实，不按训练记忆填入“最新版”。

| 范围                  | 锁定或要求                                               |
| --------------------- | -------------------------------------------------------- |
| 核心 `go.mod`         | Go 1.24.0；无第三方运行时依赖                            |
| 工作区 / 框架示例模块 | Go 1.25.0 或更高                                         |
| Fiber 示例            | `github.com/gofiber/fiber/v3 v3.5.0`                     |
| fasthttp              | `github.com/valyala/fasthttp v1.73.0`，由 Fiber 依赖引入 |
| Gin 示例              | `github.com/gin-gonic/gin v1.12.0`                       |
| Python 脚本格式工具   | Ruff 0.9.10，开发工具                                    |
| 包管理工具            | pnpm 11.15.1                                             |
| 文档格式工具          | Prettier 3.9.10，开发依赖                                |
| XML 校验              | 本机 `xmllint`，只用于测试                               |
| 辅助脚本              | Python 3，仅标准库                                       |

所有传递依赖及校验和以对应 module 的 go.mod/go.sum 为准。解析器和协议引擎不会因 Fiber/Gin 示例而引入那些框架依赖。标准 HTTP 接入代码也只存在于 `examples/internal/nethttp`，没有单独的公共适配包；标准 HTTP 与 Gin 示例共用它，不新增模块或依赖。

## 未发布的本地版本

Fiber/Gin 示例模块暂时引用核心的 `v0.0.0`；Fiber 不再拥有单独的适配器模块。工作区通过相对路径、版本限定的 replace 联调。这些替换只存在于 go.work，而不在正式 go.mod。

`make tidy` 在临时 modfile 中注入本地替换，完成整理后去掉替换，再写回依赖清单。`make consumers` 把本地源码打成临时模块代理，使用 GOWORK=off 构建独立核心消费者并确认没有第三方运行时依赖，没有永久文件路径依赖。框架示例继续参与多模块构建、测试和 race 检查，但不作为公共适配包消费。

## 升级门禁

升级 Fiber 或 fasthttp 后，应首先检查 Context 复用、SendStream 和 CloseWithError 的真实实现，再执行共享真实 TCP 测试及显式延迟读取测试。Go 最低版本应在实际最低工具链测试后调整，不能只改 go.mod 的数字。

当前测试报告说明的是本机工具链结果。其他 Go 小版本、Windows/Linux 平台及框架主版本没有自动获得同样的兼容性结论。

## Go 分析工具

`make tools` 使用 `GOTOOLCHAIN=go1.27.0` 构建固定的 `golang.org/x/tools/cmd/goimports@v0.44.1-0.20260420230617-19499e7caabc` 和 `honnef.co/go/tools/cmd/staticcheck@v0.8.0`，放在 `.artifacts/tools`。Staticcheck 该版本的工具模块要求 Go 1.26 以上；独立工具链用于工具构建，不代表将核心运行时最低版本改为 1.27。缺少固定工具链时 Go 可能按其标准机制下载它。

`make fmt` 使用 goimports 整理完整 Go 源文件，文档内 Go 代码块仍使用 gofmt。`make check` 强制检查导入格式、公共 API 注释及全部模块的 Staticcheck，不静默跳过工具缺失。CI 也已增加 `make tools`，但此次没有触发远程 CI，不能据此宣称其他工具链构建已验证。

无 CGO 构建输出统一放在 `.artifacts/check/build`，避免覆盖示例目录下已有的本地可执行文件。
