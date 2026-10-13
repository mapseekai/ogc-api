# CITE 测试接入指南

本页说明如何将由 `ogc-api`、业务 Provider 和宿主接入层组成的服务接入 OGC 测试套件。测试环境使用与目录声明一致的数据、地图内容和瓦片内容。

## 官方套件

| 套件         | 入口                                                       | 服务准备                                    |
| ------------ | ---------------------------------------------------------- | ------------------------------------------- |
| WMS 1.3.0    | [ets-wms13](https://opengeospatial.github.io/ets-wms13/)   | 按测试选项配置图层、CRS、图像格式和查询能力 |
| WMTS 1.0.0   | [ets-wmts10](https://opengeospatial.github.io/ets-wmts10/) | 配置能力文档和套件规定的测试瓦片            |
| OGC 测试平台 | [CITE](https://cite.opengeospatial.org/)                   | 提供测试执行器可访问的服务地址              |

WMS 套件提供 Blue Lake 数据集及对应图层说明。WMTS 套件提供 `noDataSquare.png`，并规定 `TransparentNodata` 图层、`Default_style` 样式、同名矩阵集、矩阵 `0` 和行列 `0/0` 的测试响应。数据配置按各套件的说明固定。

## 服务配置

通过 `server.NewSnapshot` 注册测试目录，使用 `server.Handlers` 连接实际地图与瓦片 Provider。`WMSURL`、`WMTSURL` 和 `RESTBaseURL` 设置为测试执行器可以访问的受控地址。

使用 `GetCapabilities` 核对公开的图层、样式、格式、坐标系、矩阵集、维度和操作。业务 Provider 按相同配置提供地图、瓦片或查询结果。

## 本地预检

服务在 `127.0.0.1:18080` 运行时，可执行以下能力文档检查：

```bash
curl --fail \
  'http://127.0.0.1:18080/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities' \
  -o /tmp/ogc-wms-capabilities.xml

curl --fail \
  'http://127.0.0.1:18080/wmts?SERVICE=WMTS&REQUEST=GetCapabilities' \
  -o /tmp/ogc-wmts-capabilities.xml

xmllint --nonet --noout /tmp/ogc-wms-capabilities.xml
xmllint --nonet --noout /tmp/ogc-wmts-capabilities.xml
```

仓库自身的协议、流和固定 XML 夹具可通过 `./scripts/check.sh --consumers` 一并验证。

## 执行与记录

在测试平台选择与服务一致的套件版本，填写该执行环境可访问的 GetCapabilities URL，并按服务声明选择测试选项。固定 Provider 数据集、源码版本和部署配置，保存测试会话和原始输出。

| 记录项 | 内容                               |
| ------ | ---------------------------------- |
| 套件   | 名称、发行版或 commit、测试选项    |
| 服务   | 源码版本、访问地址、宿主配置       |
| 数据   | 数据集版本、图层、样式、矩阵及维度 |
| 执行   | 时间、会话标识、执行器环境         |
| 结果   | 各测试项原始状态、断言和报告       |

协议解析、目录与 XML 的检查定位到核心包；图像尺寸、地理范围、样式和像素内容的检查定位到 Provider；网络访问与响应写出定位到宿主。按这个分工记录和复验结果。
