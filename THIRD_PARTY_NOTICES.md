# 第三方版权与许可

NeuxBot 自有代码与文档采用根目录 [MIT License](LICENSE)。以下版权及许可适用于对应的第三方组成，集中保存在本文件，并随安装包分发。

## 伙伴档案分词

Go Core 使用 [tiktoken-go/tokenizer v0.7.0](https://github.com/tiktoken-go/tokenizer/tree/v0.7.0) 的内置 `cl100k_base` 词表和 [regexp2 v1.11.5](https://github.com/dlclark/regexp2/tree/v1.11.5) 进行本地分词。两者采用 MIT 许可。

```text
Copyright (c) 2023 tiktoken-go
Copyright (c) Doug Clark

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## Material Icon Theme 文件图标

工作区文件树、消息文件引用及共用文件展示使用 [material-extensions/vscode-material-icon-theme](https://github.com/material-extensions/vscode-material-icon-theme/tree/ced21b9b5241c1efa502a8c46ec7363a690a7058) 的精选 SVG，固定提交 `ced21b9b5241c1efa502a8c46ec7363a690a7058`。类型图标从上游原样复制，通用文件和目录图标通过本项目同步命令按同一提交的上游生成器路径及默认色生成。来源与更新命令见 [NOTICE](packages/frontend/src/shared/assets/material-icon-theme/NOTICE.md)，完整许可如下：

```text
The MIT License (MIT)
Copyright (c) 2025 Material Extensions

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## Bloub 头像动画核心

通用形状、径向轮廓插值、眼睛与状态动画源于 [jeremy-prt/bloub](https://github.com/jeremy-prt/bloub)，由 [JieWaZi/bloub](https://github.com/JieWaZi/bloub) 维护，通过 `@neuxbot/bloub` 直接依赖 `main` 分支，由锁文件固定解析提交，版本以 `packages/frontend/package.json` 及根锁文件为准。`@neuxbot/bloub` 中的 `neuxbot/src/bot`、`src/ui/gaze.ts` 与 `src/plush` 采用公开 fork [NorbertBodziony/bloub](https://github.com/NorbertBodziony/bloub/tree/7983de0bb7e868ac84881581eaa8c2e1f459dd30) 的固定源码，复用框架无关几何、换色和状态辅助算法。Copyright (c) 2026 Jérémy Perret，采用 MIT；完整许可见 [MIT 许可](https://github.com/JieWaZi/bloub/blob/84b02098dbd5822ec7c5e7cd0335952e9c8560e2/neuxbot/LICENSE)。固定来源记录于 [upstream.json](https://github.com/JieWaZi/bloub/blob/84b02098dbd5822ec7c5e7cd0335952e9c8560e2/neuxbot/upstream.json)。

毛绒为 NeuxBot 独立生成的完整立体身体素材，粒子为根据通用形状三维体积取样生成的细光尘图集，不包含第三方毛绒图片或角色。材质绘图与 React 集成由本项目维护，素材来源、生成方法和用途见[材质说明](https://github.com/JieWaZi/bloub/blob/84b02098dbd5822ec7c5e7cd0335952e9c8560e2/neuxbot/materials/README.md)。

点击表情全池排除当前项的选择策略参考 [dsh-bloub-mood](https://github.com/Yuuhann1999/dsh-bloub-mood/tree/c626528b3d6c32669c265d7144e85f60f48570d2) 的 `client-template.js`，以类型化纯函数适配于 `shared/companion/interaction-expressions.ts`。Copyright (c) 2026 Yuuhann1999，MIT，完整许可见 [LICENSE.dsh-bloub-mood](https://github.com/JieWaZi/bloub/blob/84b02098dbd5822ec7c5e7cd0335952e9c8560e2/neuxbot/LICENSE.dsh-bloub-mood)。该来源与 Bloub 同为原生 16 种表情；NeuxBot 在同一目录原创增加 8 个姿态至 24 种，未复制其生成素材或另建渲染框架。固定来源见 [expressions-source.json](https://github.com/JieWaZi/bloub/blob/84b02098dbd5822ec7c5e7cd0335952e9c8560e2/neuxbot/expressions-source.json)。

## OpenBot UI 布局、页签、资源与设置界面

窗口布局、侧栏几何与页签展示交互采用 [OpenBot v0.62.0](https://github.com/nightly-labs/openbot/tree/fcb35746d4919798ff889a8a58449f5d58de5f65) 的实现，结合本项目 React、语义 Token、可访问性与既有内容所有权适配。对应范围为 `app/layout` 的框架／导航、`shared/ui/scene-tab-strip.tsx` 与 `modules/chat/workbench` 的页签展示，以及插件、技能和连接器目录的分类／已安装侧栏、两列轻量条目、紧凑身份头与单列详情分区。插件资源部分参考 `MarketplaceNav`、`MarketplaceBrowse`、`MarketplaceCards`、`MarketplaceAppPage` 与 `IntegrationLayout`；技能直接对应同一提交的 `packages/ui/src/features/marketplace/MarketplaceSkills.tsx`（SkillLibrary／LibraryRow）、`MarketplaceSkillPage.tsx`（伙伴安装、SKILL.md 与信息），并参考 `packages/ui/src/components/SkillPreview.tsx` 与 `packages/ui/src/skill-preview.css` 的预览排版及既有试用卡样式。技能列表以用户分类对应源端六项分组与分类页，不展示插件或内置技能，不沿用旧的安装来源分组；本地资源文件、历史与编辑为已有能力的适配。连接器页面内新建及技能添加页的表单结构参考同一提交的 `src/renderer/src/features/servers/ServerMcpPanel.tsx`，未保存状态、重置与提交底栏参考 `ServerSettingsModal.tsx` 的保存条；资源表单切换参考 `packages/ui/src/styles/sliding-tabs.css` 的轨道、选项与选中面，适配于共享 SegmentedControl 的独立资源表单外观；连接器仍使用本项目现有传输、信任、安装与诊断契约；其详情复用相同的共享试用卡，连接器默认提示、真实资源引用及草稿交接是本项目在该视觉基础上的扩展。对应适配位于 `modules/plugin`、`modules/skill`、`modules/connector`、资源页装配与 `shared/ui` 资源变体。角色、上下文及组件映射统一见[资源 UI 标准](docs/development/resource-ui-cn.md)。

设置侧栏、路径、分组行和智能体详情参考同一提交的 `SettingsDialogShell`、`ProviderSettingsPage`、`SettingsGeneralTab`、`settings-modal.css` 和 `window.css`，适配位于 `modules/settings` 及共享导航和设置表面变体。账号按智能体归属的 Go 持久化与校验沿用本项目领域实现。受管制品来源和布局核对同一提交的 `provider-runtime-manager.ts`、`provider-runtime-descriptors.ts`、`provider-runtime-releases.ts`、`provider-runtime-archive.ts` 及 `native-runtime.lock.json`；安装器以 Go 独立实现下载校验、完整版本发布及取消，不复制这些 TypeScript 实现。默认模型与安装支持范围以本项目真实能力为准。

该来源采用 [PolyForm Noncommercial License 1.0.0](https://polyformproject.org/licenses/noncommercial/1.0.0)，原始 [LICENSE](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/LICENSE) 及以下 Required Notice 适用于对应第三方部分，不将其推定为项目 MIT 许可：

```text
Required Notice: Copyright 2026 Synthetify Labs spółka z ograniczoną odpowiedzialnością.

Required Notice: For a commercial license, contact licenses@openbot.run.
```

## ACP Go SDK

Go 模块 `github.com/JieWaZi/acp-go-sdk` 使用独立 [官方仓库 fork](https://github.com/JieWaZi/acp-go-sdk)，从 coder/acp-go-sdk v0.13.5 迁入补丁并合并官方 main。Copyright 2025 Coder Technologies, Inc.，采用 Apache License 2.0；完整 [LICENSE](https://github.com/JieWaZi/acp-go-sdk/blob/main/LICENSE) 随安装包保存在 `licenses/acp-go-sdk/LICENSE`。

## ACP Adapter 中的第三方移植

acp-go 是项目自有适配器仓库。其自动审查、Pi 与 Cursor 移植组件保留各自的原有许可及 NOTICE，打包从实际 Go 依赖版本复制到 `licenses/acp-go/`，包括 `pkg/autoreview/UPSTREAM-LICENSE`、`pkg/pi/UPSTREAM-LICENSE` 和 `pkg/cursor/` 下的三份许可与 NOTICE，不将这些移植组件推定为统一许可。

## Dock 组件

适用范围：packages/frontend/src/shared/ui/dock.tsx。

```text
MIT License

Copyright (c) Magic UI

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## 结构化页面依赖

适用范围：@json-render/core、@json-render/react。

```text
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

1.  Definitions.

    "License" shall mean the terms and conditions for use, reproduction,
    and distribution as defined by Sections 1 through 9 of this document.

    "Licensor" shall mean the copyright owner or entity authorized by
    the copyright owner that is granting the License.

    "Legal Entity" shall mean the union of the acting entity and all
    other entities that control, are controlled by, or are under common
    control with that entity. For the purposes of this definition,
    "control" means (i) the power, direct or indirect, to cause the
    direction or management of such entity, whether by contract or
    otherwise, or (ii) ownership of fifty percent (50%) or more of the
    outstanding shares, or (iii) beneficial ownership of such entity.

    "You" (or "Your") shall mean an individual or Legal Entity
    exercising permissions granted by this License.

    "Source" form shall mean the preferred form for making modifications,
    including but not limited to software source code, documentation
    source, and configuration files.

    "Object" form shall mean any form resulting from mechanical
    transformation or translation of a Source form, including but
    not limited to compiled object code, generated documentation,
    and conversions to other media types.

    "Work" shall mean the work of authorship, whether in Source or
    Object form, made available under the License, as indicated by a
    copyright notice that is included in or attached to the work
    (an example is provided in the Appendix below).

    "Derivative Works" shall mean any work, whether in Source or Object
    form, that is based on (or derived from) the Work and for which the
    editorial revisions, annotations, elaborations, or other modifications
    represent, as a whole, an original work of authorship. For the purposes
    of this License, Derivative Works shall not include works that remain
    separable from, or merely link (or bind by name) to the interfaces of,
    the Work and Derivative Works thereof.

    "Contribution" shall mean any work of authorship, including
    the original version of the Work and any modifications or additions
    to that Work or Derivative Works thereof, that is intentionally
    submitted to Licensor for inclusion in the Work by the copyright owner
    or by an individual or Legal Entity authorized to submit on behalf of
    the copyright owner. For the purposes of this definition, "submitted"
    means any form of electronic, verbal, or written communication sent
    to the Licensor or its representatives, including but not limited to
    communication on electronic mailing lists, source code control systems,
    and issue tracking systems that are managed by, or on behalf of, the
    Licensor for the purpose of discussing and improving the Work, but
    excluding communication that is conspicuously marked or otherwise
    designated in writing by the copyright owner as "Not a Contribution."

    "Contributor" shall mean Licensor and any individual or Legal Entity
    on behalf of whom a Contribution has been received by Licensor and
    subsequently incorporated within the Work.

2.  Grant of Copyright License. Subject to the terms and conditions of
    this License, each Contributor hereby grants to You a perpetual,
    worldwide, non-exclusive, no-charge, royalty-free, irrevocable
    copyright license to reproduce, prepare Derivative Works of,
    publicly display, publicly perform, sublicense, and distribute the
    Work and such Derivative Works in Source or Object form.

3.  Grant of Patent License. Subject to the terms and conditions of
    this License, each Contributor hereby grants to You a perpetual,
    worldwide, non-exclusive, no-charge, royalty-free, irrevocable
    (except as stated in this section) patent license to make, have made,
    use, offer to sell, sell, import, and otherwise transfer the Work,
    where such license applies only to those patent claims licensable
    by such Contributor that are necessarily infringed by their
    Contribution(s) alone or by combination of their Contribution(s)
    with the Work to which such Contribution(s) was submitted. If You
    institute patent litigation against any entity (including a
    cross-claim or counterclaim in a lawsuit) alleging that the Work
    or a Contribution incorporated within the Work constitutes direct
    or contributory patent infringement, then any patent licenses
    granted to You under this License for that Work shall terminate
    as of the date such litigation is filed.

4.  Redistribution. You may reproduce and distribute copies of the
    Work or Derivative Works thereof in any medium, with or without
    modifications, and in Source or Object form, provided that You
    meet the following conditions:

    (a) You must give any other recipients of the Work or
    Derivative Works a copy of this License; and

    (b) You must cause any modified files to carry prominent notices
    stating that You changed the files; and

    (c) You must retain, in the Source form of any Derivative Works
    that You distribute, all copyright, patent, trademark, and
    attribution notices from the Source form of the Work,
    excluding those notices that do not pertain to any part of
    the Derivative Works; and

    (d) If the Work includes a "NOTICE" text file as part of its
    distribution, then any Derivative Works that You distribute must
    include a readable copy of the attribution notices contained
    within such NOTICE file, excluding those notices that do not
    pertain to any part of the Derivative Works, in at least one
    of the following places: within a NOTICE text file distributed
    as part of the Derivative Works; within the Source form or
    documentation, if provided along with the Derivative Works; or,
    within a display generated by the Derivative Works, if and
    wherever such third-party notices normally appear. The contents
    of the NOTICE file are for informational purposes only and
    do not modify the License. You may add Your own attribution
    notices within Derivative Works that You distribute, alongside
    or as an addendum to the NOTICE text from the Work, provided
    that such additional attribution notices cannot be construed
    as modifying the License.

    You may add Your own copyright statement to Your modifications and
    may provide additional or different license terms and conditions
    for use, reproduction, or distribution of Your modifications, or
    for any such Derivative Works as a whole, provided Your use,
    reproduction, and distribution of the Work otherwise complies with
    the conditions stated in this License.

5.  Submission of Contributions. Unless You explicitly state otherwise,
    any Contribution intentionally submitted for inclusion in the Work
    by You to the Licensor shall be under the terms and conditions of
    this License, without any additional terms or conditions.
    Notwithstanding the above, nothing herein shall supersede or modify
    the terms of any separate license agreement you may have executed
    with Licensor regarding such Contributions.

6.  Trademarks. This License does not grant permission to use the trade
    names, trademarks, service marks, or product names of the Licensor,
    except as required for reasonable and customary use in describing the
    origin of the Work and reproducing the content of the NOTICE file.

7.  Disclaimer of Warranty. Unless required by applicable law or
    agreed to in writing, Licensor provides the Work (and each
    Contributor provides its Contributions) on an "AS IS" BASIS,
    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
    implied, including, without limitation, any warranties or conditions
    of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
    PARTICULAR PURPOSE. You are solely responsible for determining the
    appropriateness of using or redistributing the Work and assume any
    risks associated with Your exercise of permissions under this License.

8.  Limitation of Liability. In no event and under no legal theory,
    whether in tort (including negligence), contract, or otherwise,
    unless required by applicable law (such as deliberate and grossly
    negligent acts) or agreed to in writing, shall any Contributor be
    liable to You for damages, including any direct, indirect, special,
    incidental, or consequential damages of any character arising as a
    result of this License or out of the use or inability to use the
    Work (including but not limited to damages for loss of goodwill,
    work stoppage, computer failure or malfunction, or any and all
    other commercial damages or losses), even if such Contributor
    has been advised of the possibility of such damages.

9.  Accepting Warranty or Additional Liability. While redistributing
    the Work or Derivative Works thereof, You may choose to offer,
    and charge a fee for, acceptance of support, warranty, indemnity,
    or other liability obligations and/or rights consistent with this
    License. However, in accepting such obligations, You may act only
    on Your own behalf and on Your sole responsibility, not on behalf
    of any other Contributor, and only if You agree to indemnify,
    defend, and hold each Contributor harmless for any liability
    incurred by, or claims asserted against, such Contributor by reason
    of your accepting any such warranty or additional liability.

END OF TERMS AND CONDITIONS

APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

Copyright 2025 Vercel Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## 欢迎字标渲染器

适用范围：vendor/elastic-mesh。保留文字栅格化、距离场和网格交互，适配主题、渐变、欢迎交接与资源释放。

```text
MIT + Commons Clause License Condition v1.0

Copyright (c) 2026 David Haz

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, and distribute the Software **as part of an application, website, or product**, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

## Commons Clause Restriction

You may use this Software, including for any commercial purpose, **so long as you do not sell, sublicense, or redistribute the components themselves-whether alone, in a bundle, or as a ported version.**

## No Warranty

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## 运行时安装的插件

插件包及其供应商程序依赖由独立 [neux-plugins](https://github.com/JieWaZi/neux-plugins) 仓库维护并分发。NeuxBot 安装包交付通用插件宿主，不内置供应商 CLI、插件目录、详情或安装归档。运行时下载的固定插件包保留实际许可证、来源与修改记录，其第三方组成以随附的 LICENSE、NOTICE 和 provenance 为准。

## ledongthuc/pdf 文本阅读校验

NeuxBot 的文本 PDF 覆盖核对使用 `github.com/ledongthuc/pdf`，固定版本 `v0.0.0-20260907135840-6c8c28e0e8a0`（提交 `6c8c28e0e8a0`）。来源：[项目仓库](https://github.com/ledongthuc/pdf/tree/6c8c28e0e8a0)，基于 rsc/pdf，BSD-3-Clause。仅读取文本层，不包含 OCR。以下保留原始许可：

```text
Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## 知识库

### 图谱引擎与初始几何

图谱使用 [react-force-graph-2d](https://github.com/vasturiano/react-force-graph) 及其 [force-graph](https://github.com/vasturiano/force-graph) Canvas 引擎（MIT），标准布局力来自 [d3-force](https://github.com/d3/d3-force)（ISC）。依赖包中的原始许可继续保留；图谱引擎的 MIT 许可如下。

```text
MIT License

Copyright (c) 2018 Vasco Asturiano

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

D3 force 的 ISC 许可：

```text
Copyright 2010-2021 Mike Bostock

Permission to use, copy, modify, and/or distribute this software for any purpose
with or without fee is hereby granted, provided that the above copyright notice
and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND
FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS
OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER
TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF
THIS SOFTWARE.
```

`packages/frontend/src/modules/knowledge/knowledge-graph-layout.ts` 中的初始布局、连通社区、主题名称、标签避让与轮廓几何及对应测试迁自 [Tencent/WeKnora 固定提交](https://github.com/Tencent/WeKnora/tree/8776a2d92d11ad3e76865d51cf3192e37e2a9976) 的 `frontend/src/views/knowledge/wiki/wikiGraphLayout.ts`。宿主适配节点尺寸和显式引用校验；运行中的力导向与绘制由第三方引擎负责。WeKnora 的 MIT 许可与版权见本节下方声明。

### WeKnora 知识检索与 Wiki 维护指南

`skills/ally/references/wiki-guide.md` 的检索流程参考 WeKnora 的 `progressive_rag_agent` 与 `wiki_researcher`。`skills/ally/references/wiki-maintenance.md` 的维护流程迁自 [Tencent/WeKnora 固定提交](https://github.com/Tencent/WeKnora/tree/2c0d8384398ca756ee414eef01b71caf91e2caa6) 的 `config/prompt_templates/agent_system_prompt.yaml` 中的 `wiki_fixer`。命令、版本和来源证据约束适配 NeuxBot 的 `allycli` 合同。WeKnora 的 MIT 许可与版权见本节下方声明。

### WeKnora 集成凭据与授权

应用内集成凭据及路由授权迁自 [Tencent/WeKnora 固定提交](https://github.com/Tencent/WeKnora/tree/8776a2d92d11ad3e76865d51cf3192e37e2a9976)：`internal/application/service/tenant_api_key.go` 的随机凭据、摘要与 ID 归一化迁入 `server/internal/integration/key.go`；能力和资源集合规则来自 `internal/types/tenant_api_key.go`，在 `server/internal/integration/authorization.go` 适配空范围拒绝及独立整理授权。对应创建格式、授权、轮换和到期行为由宿主测试保留。未迁入上游第三方数据库、队列和前端依赖。

```text
+Tencent is pleased to support the open source community by making  WeKnora available.

Copyright (C) 2026 Tencent.  All rights reserved.

The open-source software included in this distribution may have been modified by Tencent (“Tencent Modifications”). All Tencent Modifications are Copyright (C) Tencent.

 WeKnora is licensed under the MIT except for the third-party components listed below.


Terms of the MIT:
--------------------------------------------------------------------
Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

### 独立知识核心

知识库使用 [neux-knowledge](https://github.com/JieWaZi/neux-knowledge)，采用 MIT 许可。完整许可与第三方声明由该模块维护，构建时从所引用的模块版本复制并随安装包交付，见 [LICENSE](https://github.com/JieWaZi/neux-knowledge/blob/v0.1.0/LICENSE) 和 [THIRD_PARTY_NOTICES.md](https://github.com/JieWaZi/neux-knowledge/blob/v0.1.0/THIRD_PARTY_NOTICES.md)。

## 企业微信 IM 协议参考

适用范围：`server/internal/im/wecom*.go` 与企业微信扫码接入说明。长连接认证、心跳、消息、主动发送、媒体分片及解密协议参考企业微信团队的 [WecomTeam/aibot-node-sdk](https://github.com/WecomTeam/aibot-node-sdk)；接入行为参考其 [官方 OpenClaw 插件](https://github.com/WecomTeam/wecom-openclaw-plugin)（`2026.10.19`，MIT）。扫码生成与结果查询使用官方 [`@wecom/wecom-openclaw-cli`](https://www.npmjs.com/package/@wecom/wecom-openclaw-cli) `1.1.1`（ISC）发布包公开使用的企业微信 HTTPS 接口，来源标识为 `neuxbot`。

NeuxBot 在现有 Go IM 边界实现协议互通，不分发或执行上述 Node SDK、OpenClaw 插件和安装 CLI，不代表企业微信官方产品。消息与媒体传输复用仓库已使用的 [Gorilla WebSocket](https://github.com/gorilla/websocket) 及 Go 标准库；上游接口可能独立变化，协议测试和真实平台验收分别维护。

## 企业微信品牌图标

适用范围：`packages/frontend/src/shared/assets/brands/wecom.png`。图标原样取自[企业微信官网](https://work.weixin.qq.com/)导航使用的[官方 CDN 资源](https://wwcdn.weixin.qq.com/node/wework/images/wecom_20.c2xok7bh.png)，随应用本地加载，仅用于标识企业微信集成。企业微信名称与标识的权利归腾讯及相应权利人所有，不适用本仓库 MIT 许可，不表示官方背书。

## Qoder 与 WorkBuddy 品牌图标

适用范围：`packages/frontend/src/shared/assets/brands/qoder.svg` 与 `workbuddy.svg`。分别原样取自 [Qoder 官网 favicon](https://qoder.com/favIcon.svg) 与 [WorkBuddy 官网 CDN 图标](https://assets.workbuddy.cn/web/workbuddy/a5b2fb510f0e7b156f19d072fae0cd12ae1f2f19/assets/logo.svg)，随应用本地加载，仅用于标识对应智能体。软件名称与标识的权利归各自权利人所有，不适用本仓库 MIT 许可，不表示官方背书。

## README CLI 品牌图标

适用范围：`docs/assets/readme/multi-cli.png`。Codex、Claude Code、Cursor、Kimi、OpenCode、Gemini CLI 与 Grok 图标复用应用使用的 [LobeHub Icons](https://github.com/lobehub/lobe-icons)（`@lobehub/icons 5.15.0`，MIT）。Qoder 使用 [Qoder 官网图标](https://qoder.com/favIcon.svg)；WorkBuddy 使用[官网 CDN 图标](https://assets.workbuddy.cn/web/workbuddy/a5b2fb510f0e7b156f19d072fae0cd12ae1f2f19/assets/logo.svg)；Pi 使用 [Pi 官网图标](https://pi.dev/favicon.svg)。品牌名称与标识归各自权利人所有，仅用于标识对应 CLI，不表示官方背书。

LobeHub Icons 许可：

```text
MIT License

Copyright (c) 2023 LobeHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## macOS 工作区文件监听

适用范围：Core 的 macOS FSEvents 后端。

`github.com/fswatcher/fswatcher` v0.1.0 采用 MIT License：

```text
MIT License

Copyright (c) 2026 Yasuhiro Matsumoto

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

`github.com/ebitengine/purego` v0.10.0，Copyright 2022 The Ebitengine Authors，采用 Apache License 2.0，许可证全文见本文件“结构化页面依赖”章节。其从 Go runtime 移植的代码保留 Go Authors 版权并适用以下 BSD 许可：

```text
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## DeepSeek Harness Markdown、消息呈现与工作区快照

适用范围：`packages/frontend/src/shared/markdown/` 的解析、增量块缓存、直接 mdast 渲染、KaTeX、语言懒加载、流式高亮和可视区域控制，以及对应测试；聊天稳定节点和发布调度、`server/internal/chat/changes/` 工作区快照的设计与行为适配。来源固定为 [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness/tree/ce7073d51254c26cd63b22a60835b8f948deb447/packages/client/ui-primitives/src/markdown) commit `ce7073d51254c26cd63b22a60835b8f948deb447`。

移植对应关系：

| 上游文件                                                                                                     | 本地文件                      | 适配                                                                                              |
| ------------------------------------------------------------------------------------------------------------ | ----------------------------- | ------------------------------------------------------------------------------------------------- |
| `parse.ts`、`incremental.ts`、`cjkFriendlyStrong.ts`、`mathCompatibility.ts`、`windows-path-destinations.ts` | 同名文件                      | 保留语法、冻结边界、未闭合围栏及非追加失效                                                        |
| `local-image-syntax.ts`                                                                                      | 同名文件                      | 只保留本地图像语法恢复，文件类型识别改为本地扩展名集合                                            |
| `MarkdownText.tsx`、`render.tsx`                                                                             | 同名文件                      | 宿主注入现有链接、表格、Mermaid 与授权图片；移除 Cordis/专属 UI 和网络图片默认加载，禁用原始 HTML |
| `katex.tsx`                                                                                                  | 同名文件                      | 保留不可信公式解析；异常使用现有语义错误色                                                        |
| `highlight.ts`、`CodeBlock.tsx`、`useViewportHighlighting.ts`                                                | 同名文件                      | 保留 Shiki 3.23.0；使用现有编辑器语义 Token、共享 Button/i18n；仅可视块启动高亮，无模块预热计时器 |
| `tests/markdown-incremental.client.spec.tsx`                                                                 | `incremental.test.tsx`        | 保留追加、冻结、语法尾部预算、替换和数学/脚注收尾回归                                             |
| `tests/highlight-viewport.client.spec.tsx`                                                                   | `highlight-viewport.test.tsx` | 保留代码可视区域、懒加载及 observer 释放；不移植无本地消费者的 ReadBlock 用例                     |

`MarkdownText.module.css` 的六级标题、嵌套列表、相对行内代码及按列数选择表格宽度的排版规则适配到 `modules/chat/components/chat-message-content.tsx`、`chat-table.tsx` 和现有语义 Token；没有导入上游品牌配色或样式平台。本地图片经 `shared/workspace/workspace-markdown-image.tsx` 复用当前授权读取和精确文件观察，普通网页链接保留修饰点击原生行为。

`useViewportHighlighting.ts` 的单文档 observer 另提取到 `packages/frontend/src/shared/lib/viewport-activation.ts`，由 Markdown 高亮与独立成果卡片共用；`shared/lib/content-visibility.ts` 只表达当前 Scene 是否可见，隐藏时暂停计算和摘要读取。提取没有增加业务状态或第二个 observer 所有者。

聊天节点的稳定身份、dirty 部件及所属组更新、不同频率发布策略参考同一 commit 的 `packages/client/ui-conversation/src/client/conversation/assembly.ts` 与 `assembler.ts`。本地 `packages/frontend/src/modules/chat/components/chat-message-nodes.ts` 按当前 canonical 消息与连续 live 部件变更建立可丢弃投影；`packages/frontend/src/modules/chat/runtime/chat-event-batcher.ts` 在既有唯一消息消费者中采用本地单帧合并策略，不引入 Cordis、注册框架或第二份持久历史。

Go 工作区快照参考同一 commit 的 `packages/deliverables/workspace-changes/src/{git.ts,capture.ts,compare.ts,recorder.ts,paths.ts,numstat.ts,types.ts,index.ts}`，本地归属于 `server/internal/chat/changes/{changes.go,snapshot.go,diff.go,retention.go,open_unix.go,open_other.go}` 及对应测试。这是设计与行为的 Go 适配，不是 TypeScript 逐行翻译：采用私有 Git index/object/tree、不可变前后净变化与内容身份和预算；raw blob fast-import pack、`os.Root`/不跟随符号链接的安全读取、非 Git 影子仓库、冻结多根和子模块排除、Chat 记录持久恢复与选择性分叉、有界 LCS 及持久 hunks 为本地适配。该模块没有复制额外第三方声明，也没有因此新增 Go 依赖。

代码及测试保留以下 MIT 许可。Shiki、micromark/mdast 与 KaTeX 使用原有发布包，不复制其第三方语法或字体源码；既有静态 Streamdown 消费者继续使用原依赖。

```text
MIT License

Copyright (c) 2026 DeepSeek

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Inter Variable

资源页与应用使用 Inter Variable（Fontsource 5.3.0），字体文件采用 SIL Open Font License 1.1。字体与完整许可随 `packages/frontend/src/shared/assets/fonts/inter/` 分发，来源：https://github.com/rsms/inter 。

## Paper Shaders

`@paper-design/shaders` 0.0.80 用于资源试用卡的网格渐变，采用 Apache-2.0。Paper Shaders，Copyright 2026 Paper。Powered by Paper Shaders: https://shaders.paper.design 。完整 LICENSE／NOTICE 随依赖包分发。
