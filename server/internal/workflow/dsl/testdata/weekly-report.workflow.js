export const meta = {
  sdk: 'ally.workflow',
  title: 'weekly-report',
  description: '把本周口述要点汇总成简洁版工作周报（进展 / 风险 / 下周计划），草稿经你确认后写入本地 Markdown 文件',
  whenToUse: '每周需要提交个人工作周报时使用。输入本周原始要点（无需整理），流程负责提取事实、补齐背景、生成草稿、等你确认后定稿落盘。',
  inputSchema: {
    type: 'object',
    required: ['weekNotes'],
    properties: {
      weekNotes: {
        type: 'string',
        description: '本周原始要点：做了什么、卡在哪、下周打算。流水账、会议纪要、聊天记录片段都可以，不需要预先整理。'
      },
      role: { type: 'string', description: '你的角色 / 岗位职责，用于判断哪些事项值得进周报', default: '' },
      team: { type: 'string', description: '团队或项目名称，用于周报标题', default: '' },
      weekRange: { type: 'string', description: '周报周期，如 2026-09-28 ~ 2026-10-04；留空则按本周自动推断', default: '' },
      topics: { type: 'string', description: '需要联网检索补充背景的关键词，多个用逗号分隔；留空则不做网络检索', default: '' },
      outputDir: { type: 'string', description: '周报 Markdown 文件输出目录', default: './reports' }
    }
  },
  outputSchema: {
    type: 'object',
    required: ['reportPath', 'report', 'verified'],
    properties: {
      reportPath: { type: 'string', description: '定稿周报文件在磁盘上的路径' },
      report: { type: 'string', description: '最终周报正文（Markdown）' },
      verified: { type: 'boolean', description: '回读校验是否通过（文件存在且三段结构齐全）' },
      progressCount: { type: 'number', description: '本周进展条目数' },
      riskCount: { type: 'number', description: '风险条目数' },
      planCount: { type: 'number', description: '下周计划条目数' },
      reviewRounds: { type: 'number', description: '实际进行的草稿确认轮次' },
      droppedFacts: { type: 'array', description: '未被写进周报的采集事实及原因', items: { type: 'string' } }
    }
  },
  phases: [
    {
      id: 'collect',
      title: '信息采集',
      detail: '两个 worker 并行：一个把你的口述要点拆成带类型的事实清单（进展/风险/计划/其他）并标注证据强度；另一个按 topics 做联网背景检索补齐上下文，topics 为空时改为从要点自身推断隐含背景。',
      next: ['draft']
    },
    {
      id: 'draft',
      title: '草稿生成',
      detail: '基于事实清单和背景，生成简洁版三段式周报草稿（本周进展 / 风险与阻塞 / 下周计划），再由一个批评 worker 逐条比对采集清单，检查漏项、空话、无影响面的风险描述；发现缺口则就地补齐一版。',
      next: ['review']
    },
    {
      id: 'review',
      title: '草稿确认',
      detail: '暂停流程完整展示草稿。只有明确选择「定稿」才能落盘；其他回答进入返修，最多 3 轮，超限未获批准则返回草稿且不写入文件。',
      next: ['draft', 'write']
    },
    {
      id: 'write',
      title: '定稿落盘',
      detail: '把定稿周报写入 outputDir 下的 Markdown 文件，随后回读该文件校验三段标题齐全、条目未丢失，返回文件路径与校验结论。',
      canExit: true
    }
  ],
}

const FACTS_SCHEMA = {
  type: 'object',
  required: ['facts', 'periodGuess', 'overallSummary'],
  properties: {
    facts: {
      type: 'array',
      description: '从原始要点中抽出的原子事实，一条只讲一件事',
      items: {
        type: 'object',
        required: ['kind', 'statement', 'evidence'],
        properties: {
          kind: { type: 'string', enum: ['progress', 'risk', 'plan', 'context', 'other'], description: 'progress=已发生的产出，risk=阻碍或隐患，plan=下周要做，context=背景' },
          statement: { type: 'string', description: '一句话陈述该事实，保留原文中的具体名词、数字、系统名' },
          evidence: { type: 'string', enum: ['stated', 'implied', 'guessed'], description: 'stated=原文明确写了，implied=由原文合理推出，guessed=你自己补的，必须谨慎' },
          detail: { type: 'string', description: '补充说明：涉及的人、模块、时间点、完成度百分比等' }
        }
      }
    },
    periodGuess: { type: 'string', description: '推断出的周报周期，形如 2026-09-28 ~ 2026-10-04' },
    overallSummary: { type: 'string', description: '本周整体状况的三五句总结' },
    ambiguous: { type: 'array', description: '原文含糊、需要用户澄清的点', items: { type: 'string' } }
  }
}

const BACKGROUND_SCHEMA = {
  type: 'object',
  required: ['items', 'method'],
  properties: {
    method: { type: 'string', enum: ['web-research', 'notes-derived'], description: '本次是走联网检索还是仅从要点推断' },
    items: {
      type: 'array',
      items: {
        type: 'object',
        required: ['title', 'insight', 'whyItMatters'],
        properties: {
          title: { type: 'string' },
          insight: { type: 'string', description: '客观结论或事实，不要写成建议' },
          whyItMatters: { type: 'string', description: '这条背景和用户本周工作的关联，说明它该进风险段还是计划段' },
          source: { type: 'string', description: '联网检索时给出来源链接；纯推断时填 inferred' }
        }
      }
    }
  }
}

const DRAFT_SCHEMA = {
  type: 'object',
  required: ['title', 'period', 'oneLiner', 'progress', 'risks', 'nextWeek', 'markdown'],
  properties: {
    title: { type: 'string', description: '周报标题，含团队名与周期' },
    period: { type: 'string' },
    oneLiner: { type: 'string', description: '开头一句话总结，不超过 40 字' },
    progress: { type: 'array', items: { type: 'string' }, description: '每条一句完整陈述：做了什么 + 结果/状态，动词开头，不超过 40 字' },
    risks: {
      type: 'array',
      items: {
        type: 'object',
        required: ['text', 'impact'],
        properties: {
          text: { type: 'string', description: '风险或阻塞事实本身，不超过 40 字' },
          impact: { type: 'string', description: '影响面：拖慢什么、影响谁、到什么程度' },
          support: { type: 'string', description: '需要的支持或决策，没有则留空' }
        }
      }
    },
    nextWeek: { type: 'array', items: { type: 'string' }, description: '下周计划，每条可验证，写明预期产出' },
    markdown: { type: 'string', description: '可直接落盘的完整 Markdown 正文' },
    omitted: { type: 'array', description: '采集到但没写进周报的事实及省略原因', items: { type: 'string' } }
  }
}

const CRITIQUE_SCHEMA = {
  type: 'object',
  required: ['coverageGaps', 'wordingIssues', 'mustFix', 'verdict'],
  properties: {
    coverageGaps: { type: 'array', description: 'facts 里属于 progress/risk/plan 却在草稿中找不到对应条目的项', items: { type: 'string' } },
    wordingIssues: { type: 'array', description: '空话、无量化、无影响面的表述，附改写建议', items: { type: 'string' } },
    mustFix: { type: 'array', description: '必须修正的具体动作，按优先级排列', items: { type: 'string' } },
    verdict: { type: 'string', enum: ['pass', 'needs-revision'] }
  }
}

const WRITE_SCHEMA = {
  type: 'object',
  required: ['reportPath', 'written'],
  properties: {
    reportPath: { type: 'string', description: '实际写入的绝对或相对路径' },
    written: { type: 'boolean' },
    bytes: { type: 'number' },
    note: { type: 'string' }
  }
}

const VERIFY_SCHEMA = {
  type: 'object',
  required: ['exists', 'hasAllSections', 'ok'],
  properties: {
    exists: { type: 'boolean' },
    hasAllSections: { type: 'boolean', description: '进展/风险/下周计划三段标题都在' },
    itemMismatch: { type: 'boolean', description: '回读到的条目数量与草稿不一致' },
    issues: { type: 'array', items: { type: 'string' } },
    ok: { type: 'boolean' }
  }
}

const WAKER = { kind: 'waker', wakerId: 'fixture-waker' }
const role = args.role ?? ''
const team = args.team ?? ''
const topics = args.topics ?? ''
const requestedOutputDir = args.outputDir ?? './reports'
const outputDir = /^(?:[\\/]|[A-Za-z]:[\\/])/.test(requestedOutputDir)
  ? requestedOutputDir
  : `${flowContext.scope.workingDirectory}/${requestedOutputDir}`
const weekNotes = args.weekNotes

phase('collect')
const [facts, background] = await parallel([
  () => worker(
    `你是工作周报的信息整理环节。下面是我本周的原始记录（流水账/会议纪要/聊天片段，未经整理）：\n\n====原始记录开始====\n${weekNotes}\n====原始记录结束====\n\n我的角色：${role || '（未提供）'}\n指定的周报周期：${args.weekRange || '（未提供，请根据今天是哪一周推断，今天日期可从系统时间获取）'}\n\n请把这堆原始记录拆解成「原子事实清单」，要求：\n1. 一条事实只讲一件事，能区分「本周已发生的产出(progress)」/「当前阻碍或隐患(risk)」/「下周要做的事(plan)」/「纯背景信息(context)」/「其余(other)」，用 kind 标注。\n2. 必须保留原文里的具体信息：系统名、模块名、数字、版本号、人名、时间点、会议名。不要把它抽象成「推进了相关工作」这种废话。\n3. 逐条标注 evidence：原文写明的用 stated，从原文合理推出的用 implied，你自己猜的用 guessed（尽量少用 guessed，猜的要写清依据）。\n4. 如果某条信息含糊到无法判断状态（例如「差不多了」「等对方回复」），不要瞎补，写进 ambiguous 数组，由后续环节向用户确认。\n5. periodGuess 输出周报周期，格式 2026-09-28 ~ 2026-10-04；用户已指定就原样采用。\n6. overallSummary 用三五句说清本周整体状况：主线是什么、实际完成了什么、最大的不确定在哪。\n\n只输出结构化结果，不要输出寒暄。`,
    {
      id: 'facts', label: '把本周原始要点拆成带证据标注的事实清单',
      phase: 'collect',
      inputs: ['起始点'],
      outputs: ['一组带类型标注（progress/risk/plan/context）与证据强度的原子事实，加上推断出的周报周期和本周整体状况总结。'],
      schema: FACTS_SCHEMA,
      resolve: WAKER
    }
  ),
  () => worker(
    `你是工作周报的背景补全环节。\n\n本周要点（供你判断关联性，不要复述它）：\n${weekNotes}\n\n需要联网检索的关键词：${topics || '（未提供）'}\n\n规则：\n1. 如果关键词非空，使用 WebSearch/WebFetch 检索这些主题在本周（当前日期所在周）的公开动态，每个关键词至多两条，只要客观事实与结论，不要观点文章摘要。每条必须给 source 链接。\n2. 如果关键词为空，method 填 notes-derived，不要联网；改为从要点本身推断「隐含背景」：这个事项依赖谁、属于哪条链路、不做会牵连什么。最多 5 条，source 填 inferred。\n3. 每条 insight 是客观陈述；whyItMatters 必须说明它和用户本周工作的关联，并指明它应该进周报的风险段还是下周计划段。\n4. 与本周工作无关的热搜一律丢弃，宁少毋滥。\n\n只输出结构化结果。`,
    {
      id: 'background', label: '按关键词联网补齐本周相关背景动态',
      phase: 'collect',
      inputs: ['本周原始要点与用户提供的检索关键词'],
      outputs: ['一组带来源链接的背景条目（或要点推断出的隐含依赖），并标注每条应进风险段还是计划段。'],
      schema: BACKGROUND_SCHEMA,
      resolve: WAKER
    }
  )
], { id: 'gather' })

if (!facts) {
  log('事实清单解析失败：没有可用素材，周报流程终止。请重新运行并填写本周要点。')
  return { reportPath: '', report: '', verified: false, progressCount: 0, riskCount: 0, planCount: 0, reviewRounds: 0, droppedFacts: ['事实清单采集失败'] }
}

const factLines = facts.facts
  .map(f => `- [${f.kind}][${f.evidence}] ${f.statement}${f.detail ? '（' + f.detail + '）' : ''}`)
  .join('\n')
const bgLines = (background?.items ?? [])
  .map(b => `- ${b.title}：${b.insight}｜关联：${b.whyItMatters}｜来源：${b.source ?? 'inferred'}`)
  .join('\n')

let draft = null
let critique = null
let revisionNote = ''
let rounds = 0
let approved = false

phase('draft')

while (rounds < 3) {
  rounds++
  draft = await worker(
    `你是工作周报撰写环节。基于以下素材，写一份「简洁版」个人工作周报，三段式：本周进展 / 风险与阻塞 / 下周计划。\n\n【本周整体状况】\n${facts.overallSummary}\n\n【原子事实清单】\n${factLines}\n\n【背景与隐含依赖】\n${bgLines || '（无）'}\n\n【待澄清项】\n${(facts.ambiguous ?? []).join('\n') || '（无）'}\n\n${revisionNote ? '【上一轮用户提出的修改意见，必须逐条落实】\n' + revisionNote + '\n\n' : ''}【本轮若为返修，需修正的问题】\n${critique ? [...(critique.coverageGaps ?? []), ...(critique.mustFix ?? [])].join('\n') : '（首轮，无）'}\n\n写作要求：\n1. 我的角色：${role || '（未提供）'}；团队/项目：${team || '（未提供）'}；周期用 ${args.weekRange || facts.periodGuess}。\n2. 简洁优先：progress 每条一句、动词开头、不超过 40 字，写「做了什么 + 结果或当前状态」，例如「订单导出接口联调完成，灰度 10%」。不要出现「推进」「跟进」这类无信息量动词单独成句。\n3. 只写 evidence 为 stated/implied 的事实。evidence 为 guessed 的内容一律不得写成已完成的进展，确实有用就转成下周计划的一条。\n4. risks 每条必须带 impact（影响什么、影响谁、到什么程度）；确实需要上级或他人拍板的才写 support，没有就留空。纯情绪化抱怨不算风险，丢弃。\n5. nextWeek 每条要可验证，写明预期产出而非动作意向；与 risks 呼应——本周的阻塞应在下周计划里体现应对措施。\n6. 待澄清项不要编造答案。若影响某条表述的准确性，把该条写模糊并注明「待确认」，或直接从周报中省略并记入 omitted。\n7. omitted 里逐条列出你采集到但没写进周报的事实和省略原因，不允许无声丢弃。\n8. markdown 字段输出完整正文：# 标题、周期行、一句话总结（对应 oneLiner）、然后三个二级标题「## 本周进展」「## 风险与阻塞」「## 下周计划」，用无序列表，风险条目格式为「风险：…｜影响：…｜需要：…」。全文控制在 400 字以内，不要多余说明或脚注。`,
    {
      id: 'draft-report', label: '生成三段式简洁版周报草稿',
      phase: 'draft',
      inputs: ['事实清单与整体状况总结', '背景与隐含依赖条目', '用户上一轮的修改意见（返修轮）'],
      outputs: ['周报草稿：标题、周期、一句话总结、进展/风险/下周计划条目，以及可直接落盘的 Markdown 正文和省略清单。'],
      schema: DRAFT_SCHEMA,
      resolve: WAKER
    }
  )
  if (!draft) {
    log(`第 ${rounds} 轮草稿生成失败。`)
    continue
  }

  critique = await worker(
    `你是完整性与质量批评者，任务是找漏，不是重写。对照「采集到的事实清单」检查「周报草稿」，只报告问题：\n\n【事实清单】\n${factLines}\n\n【草稿全文】\n${draft.markdown}\n\n【草稿声明的省略项】\n${(draft.omitted ?? []).join('\n') || '（无，草稿声称一条未省）'}\n\n请检查：\n1. coverageGaps：事实清单里 kind 为 progress/risk/plan 的条目，哪些既没出现在草稿里、也没被省略清单合理说明？逐条点名。这是最重要的一项。\n2. wordingIssues：草稿里空话、无量化、无影响面的句子，抄出原句并给改写建议（一句话）。风险条目缺 impact 的也算。\n3. 是否把 evidence=guessed 的内容写成了已完成进展？是否把待澄清项写成了确定结论？如有，列入 mustFix。\n4. 格式：进展/风险/计划三段是否齐全，条目是否超过 40 字或全文超 400 字，计入 wordingIssues 或 mustFix。\n5. mustFix 按优先级排列，只写「要做什么」的可执行动作。\n6. verdict：只有当 coverageGaps 和 mustFix 都为空时才给 pass。有任何漏项或硬性错误一律 needs-revision。宁可严格。`,
    {
      id: 'critique', label: '逐条比对事实清单，检查草稿漏项与空话',
      phase: 'draft',
      inputs: ['事实清单原文', '本轮周报草稿全文与草稿自报的省略清单'],
      outputs: ['漏项清单、措辞问题、必须修正项，以及 pass 或 needs-revision 的结论。'],
      schema: CRITIQUE_SCHEMA,
      resolve: WAKER
    }
  )

  if (critique && critique.verdict === 'needs-revision' && rounds < 3) {
    log(`自检发现待修正项（漏项 ${(critique.coverageGaps ?? []).length} 条、必修 ${(critique.mustFix ?? []).length} 条），自动返修一轮。`)
    revisionNote = ''
    continue
  }
  if (critique && critique.verdict === 'needs-revision') {
    log(`已达返修上限，仍剩 ${(critique.coverageGaps ?? []).length} 条漏项，交由你在确认环节决定。`)
  }
  break
}

phase('review')
while (!approved) {
  const confirm = await askUser(
    `周报草稿已生成（第 ${rounds} 轮，${critique ? '自检结论：' + critique.verdict : '自检未完成'}）：\n\n${draft.markdown}\n\n${(critique?.coverageGaps ?? []).length ? '⚠ 自检发现素材里有 ' + critique.coverageGaps.length + ' 条事项没写进周报：\n' + critique.coverageGaps.join('\n') + '\n' : ''}${(draft.omitted ?? []).length ? '\n已省略的素材：\n' + draft.omitted.join('\n') + '\n' : ''}\n选「定稿」即写入文件；选「需要修改」我再问你改哪里；也可以直接在这条输入里写修改意见。`,
    { id: 'review-draft', label: '展示草稿并等待你定稿或提出修改', phase: 'review', kind: 'select',allowCustom: true,choices: [{ value: 'approve', label: '定稿' }, { value: 'revise', label: '需要修改' }], defaultValue: 'revise', allowSkip: false }
  )
  if (confirm.status === 'answered' && confirm.value === 'approve') {
    approved = true
    break
  }
  if (rounds >= 3) {
    log('已达最大返修轮次（3 轮），本次未获批准，不写入文件。')
    return { reportPath: '', report: draft.markdown, verified: false, progressCount: draft.progress.length, riskCount: draft.risks.length, planCount: draft.nextWeek.length, reviewRounds: rounds, droppedFacts: draft.omitted ?? [] }
  }
  let note = confirm.value
  if (confirm.value === 'revise') {
    const detail = await askUser(
      '请说明要调整的地方，例如：某条进展补上具体指标 / 把某个风险提到第一条 / 下周计划压缩到 3 条 / 语气更书面。可直接输入，多条用换行分隔。',
      { id: 'revision-note', label: '收集具体修改意见', phase: 'review' }
    )
    note = detail.status !== 'answered' ? '' : detail.value
  }
  revisionNote = note || '按自检意见补齐漏项即可'
  log('按你的意见重新生成草稿。')
  const previousDraft = draft
  try {
  draft = await worker(
    `你是工作周报返修环节。在上一版草稿基础上，按我的修改意见改，只改意见涉及的部分，其余原样保留。\n\n【上一版草稿】\n${draft.markdown}\n\n【我的修改意见（必须逐条落实）】\n${revisionNote}\n\n【可参考的原始素材】\n${factLines}\n\n要求：保持三段结构「## 本周进展」「## 风险与阻塞」「## 下周计划」；每条不超过 40 字；全文不超过 400 字；不要新增素材里没有的事实；不要因为修改而丢掉原有有效条目。若我的意见与素材冲突，以素材为准，并在 omitted 里说明。markdown 字段输出修改后的完整正文。`,
    {
      id: 'revise-report', label: '按我的修改意见返修周报草稿',
      phase: 'review',
      inputs: ['上一版草稿全文', '我在确认环节输入的修改意见', '原始事实清单'],
      outputs: ['落实修改意见后的周报草稿全文与省略说明。'],
      schema: DRAFT_SCHEMA,
      resolve: WAKER
    }
  )
  } catch (failure) {
    log({message:'返修执行失败，保留上一版草稿并重新等待决定。',failure})
    draft = previousDraft
    rounds++
    continue
  }
  if (!draft) {
    draft = previousDraft
    log('返修失败，保留上一版草稿，本次不写入文件。')
    return { reportPath: '', report: draft.markdown, verified: false, progressCount: draft.progress.length, riskCount: draft.risks.length, planCount: draft.nextWeek.length, reviewRounds: rounds, droppedFacts: draft.omitted ?? [] }
  }
  rounds++
}

phase('write')
const stamp = new Date().toISOString().slice(0, 10)
const safeTeam = (team || '个人').replace(/[^\w\u4e00-\u9fa5-]/g, '')
const fileName = `${outputDir}/周报-${safeTeam}-${draft.period.replace(/[^\w\u4e00-\u9fa5~\-]/g, '') || stamp}.md`

const written = await worker(
  `请把下面这份定稿周报写入磁盘，不要改写内容。\n\n目标路径：${fileName}（若目录不存在，先用命令行或 Write 工具的目录创建能力确保 ${outputDir} 存在）\n\n周报正文（Markdown，原样写入，不要添加前后缀）：\n${draft.markdown}\n\n写入完成后，用 Read 回读该文件确认内容一致、非空，然后按 schema 返回实际路径、是否成功和字节数。如果写入失败，written 返回 false 并在 note 里写清失败原因（权限、路径非法等），不要伪造成功。`,
  {
    id: 'write-report', label: '把定稿周报写入本地 Markdown 文件',
    phase: 'write',
    inputs: ['经确认的定稿周报 Markdown 正文', '输出目录与文件名'],
    outputs: ['实际写入的文件路径、写入成功标志与字节数。'],
    schema: WRITE_SCHEMA,
    resolve: WAKER
  }
)

const verify = await worker(
  `独立校验刚生成的周报文件，路径应为：${written?.reportPath || fileName}。请：\n1. 用 Read 读取该文件。若不存在或为空，exists=false 并把原因写进 issues。\n2. 检查是否同时包含「本周进展」「风险与阻塞」「下周计划」三个二级标题，缺一个则 hasAllSections=false。\n3. 与下面这份定稿正文逐条比对条目数量与内容是否一致，不一致则 itemMismatch=true 并在 issues 里列出差异条目（不要修正文件，只报告）。\n4. 检查是否有未替换的占位符、明显被截断的句子、乱码。\n5. 全部通过才 ok=true。\n\n【定稿正文】\n${draft.markdown}`,
  {
    id: 'verify-report', label: '回读文件校验三段结构完整且内容未丢失',
    phase: 'write',
    inputs: ['落盘环节返回的文件路径', '定稿周报正文'],
    outputs: ['文件是否存在、三段是否齐全、条目是否一致，以及整体校验结论与问题清单。'],
    schema: VERIFY_SCHEMA,
    resolve: WAKER
  }
)

if (verify && !verify.ok) {
  log(`落盘校验发现问题：${(verify.issues ?? []).join('；') || '见校验详情'}`)
} else {
  log(`周报已定稿：${written?.reportPath || fileName}（进展 ${draft.progress.length} 条 / 风险 ${draft.risks.length} 条 / 计划 ${draft.nextWeek.length} 条）`)
}

return {
  reportPath: written?.reportPath || fileName,
  report: draft.markdown,
  verified: !!(verify && verify.ok),
  progressCount: draft.progress.length,
  riskCount: draft.risks.length,
  planCount: draft.nextWeek.length,
  reviewRounds: rounds,
  droppedFacts: draft.omitted ?? []
}
