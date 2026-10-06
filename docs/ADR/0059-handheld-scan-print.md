# ADR-0059 手持、扫码与打印（ADR-0057 块 D1）

状态：已落地（契约 `platform.page.v2.107`）。

## 1. 立场

工厂一线用的是扫码枪、PDA 和标签打印机，不是鼠标。这一块不新建"移动端"，而是让同一张组合页面在三个点上长出一线能力：**输入可以是扫出来的**，**页面可以按手持终端排版**，**详情可以打成标签**。三者都只是呈现契约（`pageui/widgets.json`）的扩展，业务绑定、权限与查询不变，与 Foundry Workshop "同一模块、不同布局" 的做法一致。

## 2. 契约

- **扫码输入**：`input.inputKind = "scan"`。绑定与 `search` 完全相同（页面/浮层作用域的文本状态变量 → 同 owner 查询的 `search`），只是读法不同：扫码枪键盘楔入（输入 + Enter）即提交并全选，便于连续扫；浏览器有 `BarcodeDetector` 时提供摄像头按钮。宿主 `checkInputPresentation` 对 `scan` 用 `runtime.input.scanRequiredUIProfile`。
- **设备**：`PageDocument.device ∈ {"", "handheld"}`（`runtime.device`）。`handheld` 由前端以 `data-device` 落到页面根：单列、放大字号与点击目标、扫码框更大。不改节点树，桌面与手持是同一文档。
- **标签**：`PageDetailPresentation.barcode`（一个文本字段或 `id`，以 Code 128 B 绘制为内联 SVG）与 `print`（打印按钮；`@media print` 只打印该详情区块）。宿主 `CheckDetailBarcode` 要求字段存在且为 text。

## 3. 工作台

页面设置新增"设备"；`input` 组件的呈现下拉新增"条码扫描"；`detail` 检查器新增"条码"与"提供标签打印"。

## 4. 不做 / 后续

- 不做 ZPL 直连打印机：浏览器打印到标签纸已覆盖探针场景；ZPL 作为外部效果（E 块的外部效果通道）再议。
- 不做 QR 生成（Code 128 足够承载编码；二维码读取由 `BarcodeDetector` 覆盖）。
- WMS 探针的"收货作业"页面可把 `line` 选择改为扫码输入 + 手持设备，作为验收路线；本批未改探针定义。
