import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

const script = fileURLToPath(new URL('./check-agent-runtime.mjs', import.meta.url));

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'agent-runtime-check-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, 'go.mod'), 'module example.com/runtime\n\ngo 1.22\n');
  writeFileSync(join(root, 'runtime.go'), 'package runtime\n');
  return root;
}

function check(root) {
  return spawnSync(process.execPath, [script, root], { encoding: 'utf8' });
}

test('accepts an independent module', (t) => {
  const result = check(fixture(t));
  assert.equal(result.status, 0, result.stderr);
});

test('rejects a transitive server dependency', (t) => {
  const root = fixture(t);
  mkdirSync(join(root, 'server'));
  writeFileSync(join(root, 'server', 'go.mod'), 'module github.com/JieWaZi/NeuxBot/server\n\ngo 1.22\n');
  writeFileSync(join(root, 'server', 'server.go'), 'package server\n');
  mkdirSync(join(root, 'bridge'));
  writeFileSync(join(root, 'bridge', 'bridge.go'), 'package bridge\nimport _ "github.com/JieWaZi/NeuxBot/server"\n');
  writeFileSync(join(root, 'runtime.go'), 'package runtime\nimport _ "example.com/runtime/bridge"\n');
  writeFileSync(join(root, 'go.mod'), 'module example.com/runtime\n\ngo 1.22\n\nrequire github.com/JieWaZi/NeuxBot/server v0.0.0\nreplace github.com/JieWaZi/NeuxBot/server => ./server\n');
  const result = check(root);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /github\.com\/JieWaZi\/NeuxBot\/server/);
});

test('propagates a Go command failure', (t) => {
  const root = fixture(t);
  writeFileSync(join(root, 'runtime.go'), 'this is invalid Go\n');
  const result = check(root);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /runtime\.go/);
});
