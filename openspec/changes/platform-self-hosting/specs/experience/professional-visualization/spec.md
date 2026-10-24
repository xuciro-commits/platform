## REMOVED Requirements

本模块整体退出。地图、三维场景、图像标注与专用呈现不再随平台交付；页面契约与通用可视化仍由 `experience/workspace` 与页面规范负责。

### Requirement: 专业呈现沿原页面契约

**Reason**: 专业呈现组件退出。
**Migration**: 删除专用组件与示例；引用它们的页面改为通用部件，或作为独立能力包重新交付。

### Requirement: 坐标使用各自明确的空间

**Reason**: 地理与三维坐标语义只在已退出的组件中存在。
**Migration**: 删除坐标声明；业务记录保留原值，不再渲染。

### Requirement: 地图显示有界的原记录窗口

**Reason**: 地图视图退出。
**Migration**: 删除地图页面与资源；既有记录按原权限可读。

### Requirement: 文件和业务记录访问同时确认

**Reason**: 标注与地图资源访问随组件退出。
**Migration**: 删除资源绑定；文件与记录仍由各自归属方授权。

### Requirement: 图像标注使用原附件修订

**Reason**: 图像标注退出。
**Migration**: 删除标注记录；附件按存储规范保留。

### Requirement: 三维模型为有界自包含 GLB

**Reason**: 三维场景退出。
**Migration**: 删除模型资产与加载器；不保留隐藏渲染入口。

### Requirement: 场景映射绑定原资产和真实样本

**Reason**: 场景映射随三维退出。
**Migration**: 删除映射声明；不迁移为第二套数据模型。

### Requirement: 三维资源随组件责任释放

**Reason**: 无三维资源生命周期需要管理。
**Migration**: 删除资源释放路径；历史资源按对象存储策略清理。

### Requirement: 渲染判断与业务决定分离

**Reason**: 该要求只服务于已退出的渲染组件。
**Migration**: 无；页面与业务决定的分离仍由工作区与业务模型规范约束。
