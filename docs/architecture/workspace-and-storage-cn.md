# 工作区与存储

## 权威数据

业务 SQLite 保存伙伴、项目、账号、资源、团队和工作流等配置及状态；Chat JSONL 保存完整消息事实；memory.db 保存长期记忆与向量、关键词索引。生成视图、文件索引与查询投影不作为第二份权威内容。

Chat 的固定文件索引定位原日志范围；业务 SQLite 中的 `chat_execution_locations` 和覆盖游标只建立原 Chat/执行/提交身份到日志序号的查询关系，不保存执行状态、正文、配置或结果。精确状态查询回到这些位置读取原事实，正常缺失身份不遍历完整日志或固定索引。定位覆盖落后只追平原日志尾部；缺失、截短或损坏按原日志恢复，删除 Chat 时一起清理定位投影。

`internal/chatstorage` 按所有者派生聊天私有目录、伙伴工作目录与团队目录。普通聊天共享所属伙伴的固定 cwd，日志、附件原件和文件成果记录仍按 Chat 隔离。项目真实目录属于用户；原生支持附加目录时直接交付，不支持时使用伙伴或领域 cwd 下的受管 projects 入口；工作范围在首次接纳时冻结，目录失效会阻止继续执行，不静默改绑。

## 存储目录

产品名为 NeuxBot；应用标识与默认程序数据目录为 `com.neuxally.desktop`，受管业务文件根为 `.neuxally`。两者分别拥有程序状态与业务文件。

程序数据目录沿用桌面平台的应用数据位置，由 `ALLY_DATA_DIR` 指定；数据库 `ally.db`、`memory.db`，技能目录 `skills/`、`system-skills/`，独立账号目录 `ai-accounts/`、`account-workspaces/`，执行授权描述 `agent-contexts/`，以及 Electron 数据、程序日志和 `automatic-tasks-api.json` 均保留于此。

受管业务文件默认位于用户主目录的 `~/.neuxally/`，可以通过 `ALLY_MANAGED_FILES_DIR` 独立指定，不改变数据库和程序数据位置：

| 路径                                         | 职责                                         |
| -------------------------------------------- | -------------------------------------------- |
| `Agents/<waker-id>/` | 伙伴固定 cwd，普通聊天共享 |
| `Agents/<waker-id>/outputs/<execution-id>/` | 普通聊天本轮持久成果 |
| `Agents/<waker-id>/tmp/<execution-id>/` | 本轮中间文件与精确输入副本 |
| `<cwd>/projects/<chat-id>/` | 仅不支持原生附加目录时使用的受管项目链接入口 |
| `Conversations/<chat-id>/{log,attachments,file-results}/` | 普通与团队聊天各自的日志、附件原件及不可变文件成果 |
| `Workspaces/<thread-id>/{leader,members,shared}/` | 团队负责人、成员 cwd 与真实共享文件目录 |
| `workflow-workspaces/<workflow-id>/`         | 工作流脚本、状态、缓存和运行文件的固定根目录 |
| `workflow-files/`                            | 上传原件与 `metadata.json` 归属信息          |
| `im/{inbox,deliveries,workspaces}/`          | IM 接收附件、交付快照和默认工作区            |
| `forks/`、`deletions/`、`provisioning/`      | 分叉、删除和团队创建操作的持久记录与暂存文件 |

工作区使用 `workspace-owner.json` 标记实际所有者。`outputs/<execution-id>/` 与 `tmp/<execution-id>/` 直接复用已接纳执行的身份；附件仅将本轮明确引用的原件复制到 tmp，不能将整个聊天私有目录暴露给 CLI。账号配置与授权描述保留在程序数据目录，文件服务排除这些私有内容。智能体约定的 `.agents/`、`.claude/` 等技能发现目录保持原名。

项目入口只指向已冻结的真实目录，按 Chat 隔离，不替换或扫描删除其他聊天的入口；文件授权和成果身份仍以真实路径为准。Chat 删除通过既有删除记录清理自己拥有的入口，团队或伙伴聚合删除时由所属父目录一次清理。链接入口不等于额外的原生沙箱能力。

目录按当前布局直接读写，不执行旧目录迁移或旧名称回退。用户选择的真实项目目录与智能体自行管理的原生目录保留原位置。

`Conversations/<chat-id>/file-results/` 保存该 Chat 独有的文件成果记录、私有 Git 对象与有界差异计划，不写用户仓库的 index、refs 或对象目录。分叉复制所需附件与不可变成果，父 Chat 删除后仍可读取；删除 Chat 只清理其私有内容，不删除共享伙伴 cwd。默认采集本轮 outputs，明确冻结的真实项目继续按净变更采集。本轮显式引用的旧成果保留原路径，精确到文件参与采集与交付，父目录和同级文件不因此获得授权；只有用户要求另存时才复制。共享项目上的差异不证明某个执行独占修改。

本轮明确引用旧成果时，修订保留原文件路径，只有明确要求另存才复制新成果。经来源校验的引用文件进入同一执行的精确采集和能力交付范围，不授权其父目录或同级文件；普通编辑和原子替换按实际净变化比较，未变化不生成编辑成果。

目录采集使用仓库及祖先 `.gitignore`、有界 `.git/info/exclude` 和调用方显式排除；已跟踪文件遵循 Git 的跟踪语义。非 Git 范围应用目录内同类规则。精确文件采集不枚举父目录或读取旁文件，仍应用内部路径和调用方显式排除。为隔离宿主配置，不读取全局/系统 Git 配置，也不跟随本地配置 include 或 `core.excludesFile`；额外应用或用户忽略须显式传入，不声称已经导入宿主全局忽略列表。


## 工作流目录

工作流内部文件只使用 `workflow-workspaces/<workflow-id>/`。`scripts/` 保存草稿与从不可变版本物化的脚本，`state/` 保存跨运行配置和状态，`cache/` 保存缓存，`tmp/` 保存验证、进程和接纳前输入的临时文件，`runs/<run-id>/` 隔离每次运行的默认业务文件。冻结输入位于该工作流根内的 `workflow-inputs/<run-id>/`。目录由宿主创建并限制私有权限，内部软链接不能把这些目录改绑到根外。源码和版本 Bundle 仍是权威，磁盘脚本副本不能覆盖版本内容。

`workflow.authoring.prepare` 的 `workflow.workspace` 与 `workflow.source.get` 的 `workspace` 返回同一真实绝对目录；`flowContext.workspace` 再提供本次 `runDirectory`。这些路径来自配置的受管根，智能体不得猜测主目录或临时目录，也不得把脚本、缓存或状态路径作为业务参数让用户填写。创作与 worker 通过本次执行上下文获得目录约束；worker cwd 位于工作流临时根的 `workers/<effect-id>/`，由工作流拥有，固定工作流根作为冻结的额外授权目录。删除技术 Chat 不删除该领域工作区。

脚本与 Python 函数的进程工作目录为 `runs/<run-id>/`；宿主注入 `ALLY_WORKFLOW_ROOT`、`ALLY_WORKFLOW_SCRIPTS_DIR`、`ALLY_WORKFLOW_STATE_DIR`、`ALLY_WORKFLOW_CACHE_DIR`、`ALLY_WORKFLOW_TEMP_DIR`、`ALLY_WORKFLOW_RUN_DIR`，并固定 `TMPDIR`、`TMP`、`TEMP` 与 `XDG_CACHE_HOME`。动作不得通过环境配置覆盖保留目录变量。该机制约束宿主布局并给脚本明确存储契约；受信本地 Python 的任意绝对路径访问仍由实际 Runtime/平台权限管理。

业务成果默认放本次 `runDirectory`。运行文件的相对引用只按所属运行的 `runDirectory` 解析，再校验目录授权；缺失时不查找业务目录或工作流根下的同名文件。子流程参数按父运行解析，子流程成果按子运行解析；CLI 文件输入仍按 CLI 的当前目录冻结。用户明确选择的知识同步目录、报告目录等仍属于业务输出范围；这一选择不会移动脚本、缓存和辅助状态。文件结果只允许该工作流内部根或已批准的业务范围，其他目录不能仅凭路径字符串获得权限。此布局直接用于新执行，不增加历史路径回退或迁移。
## 团队目录

团队对话拥有 `Workspaces/<thread-id>/`。`shared/` 保存真实共享文件；负责人使用 `leader/`，其他成员使用 `members/<waker-id>/`。各 cwd 的 outputs 与 tmp 按执行隔离；各 Chat 的日志仍位于独立 Conversations 目录。外部项目保持真实路径；支持附加目录的 CLI 直接使用原生入口，不支持的 CLI 使用当前 Chat 独立的 projects 链接入口。

共享项目仍可能被多成员并发修改，调用方需协调冲突。成员 cwd 隔离不表示真实项目文件隔离。

## 文件授权

文件服务规范化真实路径，分别校验 cwd、冻结的真实项目根与当前 Chat 附件根。缓存包含 Chat 身份，不能因多个聊天共享 cwd 而混用私有附件权限。越界软链接不能扩大权限。预览、下载、编辑和执行分别校验，不把可预览等同于可执行。

上传附件在接纳时复制到受管目录，生成文件使用原 Chat 或 Run 身份定位。文件标签按真实规范路径与明确所有者复用，不能在成员切换时重新解释旧文件权限。

## 删除与恢复

删除 Chat 清理自己的 Conversations 目录；删除团队对话再清理所属 Workspaces 聚合；删除伙伴清理伙伴 cwd 及其聊天。各入口检查活跃工作、关闭原生消费者并校验目录归属；伙伴删除与新接纳互斥，普通聊天之间仍可并行接纳。文件暂存与关闭位于可重试数据库事务之外，数据库失败按既有删除记录回滚，启动恢复按权威记录存在性完成或回滚。删除项目配置始终保留真实目录。

权威写入与数据库投影的跨存储窗口通过明确提交顺序、补偿和恢复测试约束。应用启动恢复依赖持久事实，不把进程内状态当成耐久接纳。日志和文件的恢复不扩大原授权范围。
