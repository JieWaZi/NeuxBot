# 工作流语言与编译

## 来源与边界

完整声明由 server/internal/workflow/dsl 的注册目录生成到 generated/workflow.d.ts，知识页也从同源生成。运行 pnpm workflow:sdk:generate 更新，pnpm workflow:sdk:check 验证。不要复制完整声明到本文件。

DSL 使用静态可分析的源码描述业务编排，Go 编译器生成 Manifest 与耐久程序。Goja 提供解析及独立 JavaScript Function 能力，编排恢复依靠持久 IR 与检查点，不依赖 JavaScript VM 栈。

V2 标识为 ally.workflow/2。源码静态导出 meta 与可选 scripts，编排能力包含 worker、parallel、pipeline、askUser、workflow、action、functionTask 等。完整函数、参数、限制和示例通过当前能力发现入口读取。Python scripts 随 Revision 编入不可变 Bundle，按 JSON stdin/stdout 或文本输出协议执行；脚本内容不依赖工作目录中的可变文件。

## 校验

公开画布使用同一编译结果导出的 CanvasManifest；封闭节点联合与 JSON Schema 由 DSL 生成器维护，再导入 OpenAPI 生成前端类型。客户端不复制编译器或从源码猜测拓扑。

源码保存执行输入大小、UTF-8、语法、资源引用、Schema、控制流及策略校验。CAS 与请求身份防止并发覆盖和重复保存。诊断位置保留原源码的 UTF-8 与 UTF-16 关系。

节点身份由源码中的稳定标识提供，不能以标题或行号替代。纯语言与 JSON 值语义有封闭边界；一般 JavaScript、Python 或命令逻辑进入明确的 Function／Action。

Schema 约束定义输入、输出及真实文件引用。显示页面和输入表单分别限制能力，编译和接纳同时校验，不能依靠删除未知属性静默接受非法内容。

## 执行限制

伙伴节点没有独立短超时。脚本及 Function 使用明确的进程期限，HTTP 有独立请求期限，Run 有总期限；当前默认和最大值以 constants 与生成知识为准。

JavaScript Function 在独立进程执行纯计算。Python Function 需要 trusted_local 执行模式；内嵌脚本在接纳时解析并冻结解释器路径。本地命令和受信 Python 以当前用户权限运行，可以访问其文件、网络和进程。环境过滤、期限与内存限制不构成操作系统沙箱；批准目录是工作配置，不能宣称为系统级隔离。

## 创作契约

创作优先使用 MCP workflow_authoring_prepare 或 CLI workflow authoring prepare 按实际调用组合必要指南、源码与保存契约；缺失部分按需 describe 或读取主题。source save 内部完成编译和 CAS，成功回执即表示已保存。

资源来自当前获准目录，创作授权不自动包含运行授权。worker 只提交当前 Attempt 的候选结果。运行状态和输出由真实回执确认，不从模型的最终回复推断。
