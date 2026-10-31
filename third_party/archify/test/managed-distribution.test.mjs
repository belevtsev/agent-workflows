import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

import { startDeliveryUpdateCheck } from '../bin/delivery-update.mjs';
import { openArtifact } from '../bin/open-artifact.mjs';
import { startPreview } from '../bin/preview.mjs';
import { checkForUpdate, setUpdatePreference } from '../scripts/check-update.mjs';
import { assertExternalOutputPath } from '../renderers/shared/output-path.mjs';
import { captureAtomicOutput } from '../renderers/shared/atomic-output.mjs';
import { captureBrandReference, prepareDiagramBrandMarks } from '../renderers/shared/brand-marks.mjs';
import { BRAND_MARKS } from '../renderers/shared/generated-brand-marks.mjs';

const skillRoot = fileURLToPath(new URL('../', import.meta.url));
const cases = [
  ['architecture', 'web-app.architecture.json'],
  ['workflow', 'agent-tool-call.workflow.json'],
  ['sequence', 'cache-miss-request.sequence.json'],
  ['dataflow', 'product-analytics.dataflow.json'],
  ['lifecycle', 'agent-run.lifecycle.json'],
];

function scratch(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'archify-managed-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const home = path.join(directory, 'isolated-home');
  fs.mkdirSync(home);
  return { directory, env: {
    ...process.env, HOME: home, XDG_CACHE_HOME: path.join(home, '.cache'),
    XDG_STATE_HOME: path.join(home, '.state'), ARCHIFY_UPDATE_CACHE_DIRECTORY: path.join(home, 'updates'),
  } };
}

function hashes(directory) {
  const found = {};
  function visit(current) {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const file = path.join(current, entry.name);
      if (entry.isDirectory()) visit(file);
      else if (entry.isFile()) found[path.relative(directory, file)] = createHash('sha256').update(fs.readFileSync(file)).digest('hex');
      else if (entry.isSymbolicLink()) found[path.relative(directory, file)] = `symlink:${fs.readlinkSync(file)}`;
    }
  }
  visit(directory);
  return found;
}

function invoke(root, args, workspace, env, timeout = 30000) {
  return spawnSync(process.execPath, [path.join(root, 'bin/archify.mjs'), ...args], {
    cwd: workspace, env, encoding: 'utf8', timeout,
  });
}

function assertSuccess(result) {
  assert.equal(result.error, undefined, result.error?.message);
  assert.equal(result.status, 0, result.stderr || result.stdout);
}

function assertRefused(result) {
  assert.equal(result.error, undefined, result.error?.message);
  assert.notEqual(result.status, 0, result.stdout);
  assert.match(result.stderr + result.stdout, /managed Archify package|explicit external --output-dir/);
}

test('managed update exports and direct helper CLIs perform no network or cache work', async t => {
  const { directory, env } = scratch(t);
  const before = hashes(directory);
  const options = {
    cacheDirectory: path.join(directory, 'forbidden-cache'),
    releasePath: path.join(directory, 'missing-release.json'),
    get fetchImpl() { throw new Error('managed check accessed a network option'); },
    get now() { throw new Error('managed check accessed the clock'); },
  };
  const expected = { status: 'silent', reason: 'managed-by-agent-workflows' };
  assert.deepEqual(await checkForUpdate(options), expected);
  assert.deepEqual(await setUpdatePreference(options), expected);
  const checker = path.join(directory, 'forbidden-checker.mjs');
  fs.writeFileSync(checker, `throw new Error('managed delivery spawned an updater');\n`);
  const delivery = await startDeliveryUpdateCheck({ env, checkerPath: checker, deadlineMs: 0 });
  assert.deepEqual(delivery, {
    status: 'unavailable', installedVersion: null, availableVersion: null,
    releaseNotes: null, checkedAt: null, source: null, noticeRequired: false,
    noticeText: null, reason: 'managed-by-agent-workflows',
  });
  fs.rmSync(checker);
  const preload = path.join(directory, 'network-tripwire.mjs');
  fs.writeFileSync(preload, `import http from 'node:http';\nimport https from 'node:https';\nconst fail = () => { throw new Error('unexpected network operation'); };\nglobalThis.fetch = fail; http.request = fail; https.request = fail;\n`);
  for (const [helper, args] of [
    ['check-update.mjs', []], ['check-update.mjs', ['--snooze', 'any-event']],
    ['check-update.mjs', ['--ignore', 'any-event']], ['check-update.mjs', ['--ack', 'any-event']],
    ['delivery-update-child.mjs', []], ['delivery-update-child.mjs', ['0']],
  ]) {
    const result = spawnSync(process.execPath, ['--import', preload, path.join(skillRoot, 'scripts', helper), ...args], {
      cwd: directory, env: { ...env, ARCHIFY_UPDATE_CHECK_DISABLED: '1' }, encoding: 'utf8', timeout: 5000,
    });
    assertSuccess(result);
    assert.deepEqual(JSON.parse(result.stdout), expected);
  }
  fs.rmSync(preload);
  assert.deepEqual(hashes(directory), before);
});

test('physical output containment rejects installed files and future children through symlinks', t => {
  const { directory } = scratch(t);
  const before = hashes(skillRoot);
  const alias = path.join(directory, 'package-alias');
  fs.symlinkSync(skillRoot, alias, 'dir');
  for (const output of [
    path.join(skillRoot, 'examples/web-app-rendered.html'),
    path.join(skillRoot, 'future/missing/diagram.html'),
    path.join(alias, 'examples/web-app-rendered.html'),
    path.join(alias, 'future/missing/diagram.html'),
  ]) {
    assert.throws(() => assertExternalOutputPath(output), /outside the managed Archify package/);
    assert.throws(() => captureAtomicOutput(output), /outside the managed Archify package/);
  }
  const external = path.join(directory, 'future/missing/diagram.html');
  assert.equal(assertExternalOutputPath(external), external);
  assert.deepEqual(hashes(skillRoot), before);
});

test('resource scripts require external output directories; check modes never modify the package', t => {
  const { directory, env } = scratch(t);
  const before = hashes(skillRoot);
  const alias = path.join(directory, 'package-alias');
  fs.symlinkSync(skillRoot, alias, 'dir');
  for (const helper of ['render-examples.mjs', 'generate-validators.mjs', 'generate-brand-marks.mjs']) {
    const script = path.join(skillRoot, 'scripts', helper);
    for (const args of [[], ['--output-dir', path.join(alias, 'not-created/child')]]) {
      assertRefused(spawnSync(process.execPath, [script, ...args], { cwd: directory, env, encoding: 'utf8' }));
    }
    if (helper !== 'render-examples.mjs') {
      const aliasedFiles = path.join(directory, `file-alias-${helper}`);
      fs.mkdirSync(aliasedFiles);
      const generatedName = helper === 'generate-validators.mjs' ? 'generated-validators.mjs' : 'generated-brand-marks.mjs';
      fs.symlinkSync(path.join(skillRoot, 'renderers/shared', generatedName), path.join(aliasedFiles, generatedName), 'file');
      assertRefused(spawnSync(process.execPath, [script, '--output-dir', aliasedFiles], { cwd: directory, env, encoding: 'utf8' }));
      const result = spawnSync(process.execPath, [script, '--check'], { cwd: directory, env, encoding: 'utf8' });
      assert.ok(result.status === 0 || /Optional .* generator dependency .* is unavailable/.test(result.stderr), result.stderr);
      const external = path.join(directory, helper);
      const generated = spawnSync(process.execPath, [script, '--output-dir', external], { cwd: directory, env, encoding: 'utf8' });
      assert.ok(generated.status === 0 || /Optional .* generator dependency .* is unavailable/.test(generated.stderr), generated.stderr);
      if (generated.status === 0) assert.ok(fs.readdirSync(external).some(name => name.endsWith('.mjs')));
      else assert.equal(fs.existsSync(external), false, 'missing optional dependencies must not create a destination');
    }
  }
  assert.deepEqual(hashes(skillRoot), before);
});

test('relocated zero-install package renders and delivers all five modes with unchanged installed hashes', t => {
  const { directory, env } = scratch(t);
  const installed = path.join(directory, 'relocated-skill');
  fs.cpSync(skillRoot, installed, { recursive: true, filter: source => !['test', 'node_modules', '.git'].includes(path.relative(skillRoot, source).split(path.sep)[0]) });
  const before = hashes(installed);
  const workspace = path.join(directory, 'working');
  fs.mkdirSync(workspace);
  assertSuccess(invoke(installed, ['doctor'], workspace, env));
  for (const [mode, fixture] of cases) {
    const input = path.join(installed, 'examples', fixture);
    const rendered = path.join(workspace, `rendered-${mode}.html`);
    assertSuccess(invoke(installed, ['render', mode, input, rendered], workspace, env));
    assert.match(fs.readFileSync(rendered, 'utf8'), /<svg\b/);
    const delivered = path.join(workspace, `delivered-${mode}.html`);
    const result = invoke(installed, ['deliver', mode, input, delivered, '--json'], workspace, env);
    assertSuccess(result);
    const receipt = JSON.parse(result.stdout);
    assert.equal(receipt.ok, true);
    assert.equal(receipt.update.reason, 'managed-by-agent-workflows');
    assert.equal(receipt.update.noticeRequired, false);
    assertSuccess(invoke(installed, ['check', delivered, '--require-provenance', '--json'], workspace, env));
  }
  const examples = path.join(workspace, 'examples');
  assertSuccess(invoke(installed, ['examples', '--output-dir', examples], workspace, env));
  assert.equal(fs.readdirSync(examples).filter(name => name.endsWith('.html')).length, 15);
  assertSuccess(invoke(installed, ['demo', path.join(workspace, 'demo')], workspace, env));
  assert.deepEqual(hashes(installed), before);
  assert.deepEqual(fs.readdirSync(env.HOME), [], 'automatic runtime helpers must not create user-home state');
});

test('runtime helper entrypoints reject package outputs before modifying installed files', t => {
  const { directory, env } = scratch(t);
  const before = hashes(skillRoot);
  const alias = path.join(directory, 'package-alias');
  fs.symlinkSync(skillRoot, alias, 'dir');
  const input = path.join(skillRoot, 'examples/web-app.architecture.json');
  const output = path.join(alias, 'future/diagram.html');
  const workflow = path.join(skillRoot, 'examples/agent-tool-call.workflow.json');
  for (const args of [
    ['render', 'architecture', input, output],
    ['deliver', 'architecture', input, output, '--json'],
    ['finalize', 'architecture', input, output, '--json'],
    ['examples', '--output-dir', path.dirname(output)],
    ['demo', path.dirname(output)],
    ['preview', 'architecture', input, output],
    ['compare', 'architecture', path.join(skillRoot, 'examples/checkout-platform.base.architecture.json'), path.join(skillRoot, 'examples/checkout-platform.head.architecture.json'), output, '--json'],
    ['migrate', 'workflow', workflow, path.join(alias, 'future/workflow.json'), '--to-schema', '2', '--json'],
  ]) assertRefused(invoke(skillRoot, args, directory, env));
  for (const helper of ['browser-check', 'visual-check']) {
    assertRefused(invoke(skillRoot, [helper, path.join(skillRoot, 'examples/web-app-rendered.html'), '--out-dir', path.dirname(output), '--json'], directory, env));
  }
  assertRefused(spawnSync(process.execPath, [path.join(skillRoot, 'bin/recover-output.mjs'), path.join(alias, 'future'), '--json'], { cwd: directory, env, encoding: 'utf8' }));
  assert.deepEqual(hashes(skillRoot), before);
});

test('missing Chrome never produces a passing finalize receipt', t => {
  const { directory, env } = scratch(t);
  const before = hashes(skillRoot);
  const result = invoke(skillRoot, ['finalize', 'architecture', path.join(skillRoot, 'examples/web-app.architecture.json'), path.join(directory, 'diagram.html'), '--quality', 'showcase', '--json'], directory, { ...env, ARCHIFY_CHROME: path.join(directory, 'missing-chrome') });
  assert.notEqual(result.status, 0, result.stdout);
  const receipt = JSON.parse(result.stdout);
  assert.equal(receipt.ok, false);
  assert.notEqual(receipt.gates['browser-check'], 'pass');
  assert.equal(receipt.update, undefined);
  assert.deepEqual(hashes(skillRoot), before);
});

test('explicit custom brand URLs still require and verify their digest using a loopback fixture', async t => {
  const before = hashes(skillRoot);
  const oldPrivate = process.env.ARCHIFY_BRAND_ALLOW_PRIVATE;
  process.env.ARCHIFY_BRAND_ALLOW_PRIVATE = '1';
  t.after(() => { if (oldPrivate === undefined) delete process.env.ARCHIFY_BRAND_ALLOW_PRIVATE; else process.env.ARCHIFY_BRAND_ALLOW_PRIVATE = oldPrivate; });
  const icon = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
  const server = http.createServer((_request, response) => { response.writeHead(200, { 'content-type': 'image/png' }); response.end(icon); });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  const url = `http://127.0.0.1:${server.address().port}/mark.png`;
  const captured = await captureBrandReference(url);
  assert.equal(captured.brand.url, url);
  assert.match(captured.brand.sha256, /^[a-f0-9]{64}$/);
  await prepareDiagramBrandMarks('architecture', { components: [{ id: 'local', brand: captured.brand }] });
  await assert.rejects(prepareDiagramBrandMarks('architecture', { components: [{ id: 'changed', brand: { ...captured.brand, sha256: '0'.repeat(64) } }] }), /digest changed/);
  await assert.rejects(prepareDiagramBrandMarks('architecture', { components: [{ id: 'unpinned', brand: url }] }), /unpinned URL/);
  assert.deepEqual(hashes(skillRoot), before);
});

test('preview delivers to an external workspace and opener helpers remain injectable without launching UI', { timeout: 15000 }, async t => {
  const { directory } = scratch(t);
  const before = hashes(skillRoot);
  const output = path.join(directory, 'preview.html');
  const preview = await startPreview({
    type: 'architecture', input: path.join(skillRoot, 'examples/web-app.architecture.json'),
    output, cwd: directory, open: false, pollMs: 50,
  });
  t.after(async () => { await preview.stop(); await preview.closed; });
  const deadline = Date.now() + 10000;
  while (!fs.existsSync(output) && preview.state().status !== 'failed' && Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 20));
  }
  assert.ok(fs.existsSync(output), JSON.stringify(preview.state()));
  assert.equal(preview.state().failure, null);
  for (const platform of ['darwin', 'linux', 'win32']) {
    const calls = [];
    const result = openArtifact(output, { platform, spawn: (...args) => { calls.push(args); return { status: 0 }; } });
    assert.equal(result.status, 'opened');
    assert.equal(calls.length, 1);
    assert.equal(calls[0][2].shell, false);
  }
  await preview.stop();
  await preview.closed;
  assert.deepEqual(hashes(skillRoot), before);
});

test('font and individual mark licenses retain their separate notices and provenance', () => {
  const notices = fs.readFileSync(path.join(skillRoot, 'THIRD_PARTY_NOTICES.md'), 'utf8');
  const licensed = BRAND_MARKS.filter(mark => mark.provenance.license);
  assert.equal(licensed.length, 8);
  for (const mark of licensed) {
    assert.ok(notices.includes(mark.title), `${mark.id} title missing`);
    assert.ok(notices.includes(mark.provenance.source), `${mark.id} source missing`);
    assert.ok(notices.includes(mark.provenance.license.type), `${mark.id} license missing`);
  }
  assert.match(notices, /does not.*every packaged mark.*cleared/s);
  assert.match(fs.readFileSync(path.join(skillRoot, 'assets/JetBrainsMono-OFL.txt'), 'utf8'), /SIL OPEN FONT LICENSE Version 1.1/);
  assert.equal(JSON.parse(fs.readFileSync(path.join(skillRoot, 'skill-release.json'))).version, '3.2.0');
});
