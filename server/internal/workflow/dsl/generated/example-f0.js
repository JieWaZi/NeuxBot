export const meta = {
  sdk: 'ally.workflow', title: '纯计算验收', description: '组合输入并返回真实计算结果',
  inputSchema: { type: 'object', required: ['text'], properties: {
    text: { type: 'string' }, repeat: { type: 'integer', minimum: 1, maximum: 5, default: 2 },
    uppercase: { type: 'boolean', default: true }, suffix: { type: 'string', default: '!' }
  } },
  outputSchema: { type: 'object', required: ['message', 'length', 'repeat'], properties: {
    message: { type: 'string' }, length: { type: 'integer' }, repeat: { type: 'integer' }
  } },
  phases: [
    { id: 'prepare', title: '准备输入', detail: '按用户参数转换文本', next: ['compose'] },
    { id: 'compose', title: '组合结果', detail: '重复并返回结果', canExit: true }
  ]
};
phase('prepare');
const text = args.uppercase ? args.text.toUpperCase() : args.text;
let parts = [];
for (let index = 0; index < args.repeat; index++) { parts.push(text); }
phase('compose');
const message = parts.join(' ') + args.suffix;
return { message, length: message.length, repeat: args.repeat };
