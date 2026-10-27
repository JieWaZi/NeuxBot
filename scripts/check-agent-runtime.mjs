import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const directory = process.argv[2] ?? fileURLToPath(new URL('../packages/agent-runtime/', import.meta.url));
const env = { ...process.env, GOWORK: 'off' };

function go(args) {
  const result = spawnSync('go', args, { cwd: directory, env, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }
  if (result.stderr) process.stderr.write(result.stderr);
  if (result.status !== 0) process.exit(result.status ?? 1);
  return result.stdout;
}

const packages = go(['list', '-f', '{{.ImportPath}}', './...']).trim().split('\n').filter(Boolean);
if (packages.length === 0) {
  console.error('agent-runtime module contains no packages');
  process.exit(1);
}
const dependencies = go(['list', '-deps', '-test', '-f', '{{.ImportPath}}', ...packages]).trim().split('\n');
const forbidden = 'github.com/JieWaZi/NeuxBot/server';
for (const dependency of dependencies) {
  if (dependency === forbidden || dependency.startsWith(`${forbidden}/`)) {
    console.error(`agent-runtime must not depend on ${dependency}`);
    process.exit(1);
  }
}
for (const command of ['vet', 'test']) {
  const output = go([command, ...packages]);
  if (output) process.stdout.write(output);
}
