# 内核契约 (v1alpha1)

面向业务系统的语言中立契约 (ADR-0002)。内核将模式、语义、错误、兼容性、一致性与范畴定义为各自独立的部分。本模块严禁出现任何领域专属词汇。

## 目录结构

- `proto/`：Protobuf 模式定义 (`platform.kernel.v1alpha1`)，由 `buf lint` 进行校验。
- `spec/`：附带错误码的语义规则 ([contract/spec/README.md](spec/README.md))。
- `vectors/`：JSON 格式的语言中立一致性测试向量。
- `go/`：Go 语言参考实现 (`platform/kernel`)，验证所有测试向量。
