# CITE 联合验证入口

## 当前状态

本次已经运行项目内部的解析、目录、协议 XML 和跨框架 HTTP 测试；没有运行完整 OGC CITE 服务认证测试，也没有申请认证。

官方入口：

- WMS 1.3.0 测试范围与数据：<https://opengeospatial.github.io/ets-wms13/>
- WMTS 1.0.0 测试范围与数据：<https://opengeospatial.github.io/ets-wmts10/>
- OGC 在线测试入口：<https://cite.opengeospatial.org/>

## 接入步骤

先通过 Provider 将真实地图和瓦片能力挂到本库，而不是使用 `examples/demo` 的固定图片。WMS 测试涉及指定的 Blue Lake 图层、地图范围/尺寸和像素内容；WMTS 测试说明也要求配置指定图层、样式、矩阵与测试瓦片。

本地先确认能力文档可达：

```bash
curl --fail \
  'http://127.0.0.1:8080/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities' \
  -o /tmp/ogc-wms-capabilities.xml

curl --fail \
  'http://127.0.0.1:8080/wmts?SERVICE=WMTS&REQUEST=GetCapabilities' \
  -o /tmp/ogc-wmts-capabilities.xml

xmllint --nonet --noout /tmp/ogc-wms-capabilities.xml
xmllint --nonet --noout /tmp/ogc-wmts-capabilities.xml
```

之后在 OGC TEAM Engine/CITE 中选择相应测试套件，输入测试环境能够访问的 GetCapabilities URL，并选择部署确实声明的可选能力。公共在线测试器不能访问本机 127.0.0.1；需要由项目方明确提供受控测试环境，不能由本库自动暴露本机端口。

固定测试套件发行版、Provider 数据集和服务版本，保存 suite 名称、版本、会话标识、通过/失败/未执行项及原始报告。不要把“XML 能解析”或“前端显示图片”记录为 CITE 通过。

## 责任划分

协议库负责 URL 参数、版本语义、能力及异常文档；Provider 负责实际图像尺寸、地理范围、样式效果、像素/要素内容与数据集。完整服务测试发现的问题需要区分两个责任边界再修复。

对于尚未配置指定数据集、未启动 TEAM Engine 或无法访问被测服务的环境，报告应写“未执行”，不写“通过”。本仓库不提供一个返回固定假结果的 CITE 通过脚本。
