# 伙伴形象渲染器

styles.json 定义风格、固定身份和材质配色；assets/bodies 提供 256／512 透明身体图片，source 提供眼睛、动作、状态和命令式渲染实现。图片按显示尺寸与像素密度选择，材质颜色不改变页面语义 Token。

assets/static 提供由同一渲染源码导出的完整静态 SVG 图片，内嵌身体与固定眼睛。聊天历史头像以普通 img 按需加载这些图片，不创建动画引擎；仅最近一条助手消息保留动态头像，名称始终展示。

前端通过 renderer 的公开接口挂载、更新状态和释放。隐藏窗口、离屏与减少动态分别控制动画；头像身份保存身体风格，任务状态只改变过程表现。

静态头像、renderer.js 与 renderer.d.ts 由 scripts/build-companion.mjs 生成，修改 source 或样式目录后运行 pnpm companion:generate，再运行 pnpm companion:check。身体图生成说明保存在资源目录，运行时不加载原始制作素材。
