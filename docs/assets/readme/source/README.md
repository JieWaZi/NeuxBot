# README 图片源文件

`hero-layout.svg` 展示伙伴协作与成果，`multi-cli-layout.svg` 展示伙伴身份与 CLI 选择。两张图均为产品能力示意，不是应用截图。排版采用当前品牌标志与中性色，PNG 使用两倍分辨率导出。

三个透明伙伴 PNG 直接来自当前 `@neuxbot/bloub` 依赖的毛绒图集、染色算法、几何、表情和产品头像绘制逻辑。紫色伙伴跟随产品默认配置，另外两个使用当前头像预设。CLI 图标复用产品的 `RuntimeCliIcon`。第三方素材许可见根目录 [第三方声明](../../../../THIRD_PARTY_NOTICES.md)。

修改排版或文案时，编辑 [生成脚本](../../../../scripts/generate-readme-assets.mjs)，再生成全部源文件与发布 PNG，避免手工修改生成结果。先安装项目依赖，并在制图环境提供 `sharp`；可以通过 `NODE_PATH` 指向独立工具环境的 `node_modules`，无需修改应用依赖。

```sh
node scripts/generate-readme-assets.mjs
```

命令从仓库根目录执行，不启动应用、浏览器或模型调用。SVG 排版源使用相邻透明 PNG；README 只引用最终合成的 PNG。
