const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');
const { resolveResearchData, snapshotSQLite } = require('../research-data.cjs');
const { createUpdateBackup } = require('../data-protection.cjs');

const bundled = path.resolve(__dirname, '../resources/hermes-runtime', process.platform === 'win32' ? 'python/python.exe' : 'venv/bin/python');
const python = fs.existsSync(bundled) ? bundled : process.platform === 'win32' ? 'python' : 'python3';
const pythonAvailable = spawnSync(python, ['-I', '-c', 'import sqlite3'], { windowsHide: true }).status === 0;
function fixture() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-research-data-'));
  const userDataPath = path.join(root, 'desktop'); const configDir = path.join(root, 'config');
  const source = path.join(configDir, 'easy-stock', 'stock-research.db');
  fs.mkdirSync(path.dirname(source), { recursive: true });
  return { root, userDataPath, configDir, source, python };
}
function sqlite(db, script) {
  const result = spawnSync(python, ['-I', '-c', `import sqlite3,sys\nc=sqlite3.connect(sys.argv[1])\n${script}\nc.close()`, db], { encoding: 'utf8', windowsHide: true });
  assert.equal(result.status, 0, result.stderr);
  return result.stdout.trim().replace(/\r\n/g, '\n');
}

test('legacy research history migrates consistently and source remains intact', { skip: !pythonAvailable }, () => {
  const item = fixture(); sqlite(item.source, 'c.execute("CREATE TABLE history(value)")\nc.execute("INSERT INTO history VALUES (42)")\nc.commit()');
  const result = resolveResearchData(item);
  assert.equal(result.state, 'migrated');
  assert.equal(sqlite(result.path, 'print(c.execute("SELECT value FROM history").fetchone()[0])'), '42');
  assert.equal(sqlite(item.source, 'print(c.execute("SELECT value FROM history").fetchone()[0])'), '42');
  assert.ok(fs.existsSync(path.join(item.userDataPath, 'stock-research-migration.json')));
  assert.equal(resolveResearchData(item).state, 'local');
});

test('existing target and isolated profile preserve old history without merging or overwrite', () => {
  const item = fixture(); fs.writeFileSync(item.source, 'old');
  const isolated = resolveResearchData({ ...item, isolated: true });
  assert.equal(isolated.state, 'isolated-source-preserved'); assert.equal(fs.existsSync(isolated.path), false);
  fs.writeFileSync(isolated.path, 'current');
  const existing = resolveResearchData(item);
  assert.equal(existing.state, 'preserved-both');
  assert.equal(fs.readFileSync(existing.path, 'utf8'), 'current');
  assert.equal(fs.readFileSync(item.source, 'utf8'), 'old');
});

test('legacy migration failure cannot silently replace history with an empty database', () => {
  const item = fixture(); fs.writeFileSync(item.source, 'old');
  const result = resolveResearchData({ ...item, python: '' });
  assert.equal(result.state, 'migration-deferred');
  assert.equal(result.path, item.source);
  assert.equal(fs.existsSync(path.join(item.userDataPath, 'stock-research.db')), false);
  assert.equal(fs.readFileSync(item.source, 'utf8'), 'old');
});

test('explicit external research DB is respected and included in update backup', { skip: !pythonAvailable }, async () => {
  const item = fixture(); sqlite(item.source, 'c.execute("CREATE TABLE history(value)")\nc.execute("INSERT INTO history VALUES (99)")\nc.commit()');
  const result = resolveResearchData({ ...item, configuredPath: item.source });
  assert.equal(result.path, item.source); fs.mkdirSync(item.userDataPath);
  const backup = await createUpdateBackup({ userDataPath: item.userDataPath, fromVersion: '1', toVersion: '2', researchDBPath: result.path, sqlitePython: python });
  assert.equal(sqlite(path.join(backup.path, 'data', 'stock-research-external.db'), 'print(c.execute("SELECT value FROM history").fetchone()[0])'), '99');
  assert.ok(backup.manifest.files.some(file => file.path === 'stock-research-external.db'));
});

test('snapshot destination is never overwritten', { skip: !pythonAvailable }, () => {
  const item = fixture(); sqlite(item.source, 'c.execute("CREATE TABLE history(value)")');
  const destination = path.join(item.root, 'target.db'); fs.writeFileSync(destination, 'keep');
  assert.throws(() => snapshotSQLite({ python, source: item.source, destination }), /EEXIST/);
  assert.equal(fs.readFileSync(destination, 'utf8'), 'keep');
});

test('SQLite backup includes committed WAL history while the source connection stays open', { skip: !pythonAvailable }, async () => {
  const item = fixture();
  const { spawn } = require('node:child_process');
  const writer = spawn(python, ['-I', '-u', '-c', 'import sqlite3,sys,time\nc=sqlite3.connect(sys.argv[1])\nc.execute("PRAGMA journal_mode=WAL")\nc.execute("CREATE TABLE history(value)")\nc.execute("INSERT INTO history VALUES (123)")\nc.commit()\nprint("ready",flush=True)\ntime.sleep(15)', item.source], { stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
  try {
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('SQLite WAL fixture did not become ready')), 5000);
      writer.stdout.once('data', () => { clearTimeout(timer); resolve(); });
      writer.once('error', error => { clearTimeout(timer); reject(error); });
      writer.once('exit', code => { if (code) { clearTimeout(timer); reject(new Error('SQLite fixture exited')); } });
    });
    assert.ok(fs.existsSync(`${item.source}-wal`));
    const result = resolveResearchData(item);
    assert.equal(sqlite(result.path, 'print(c.execute("SELECT value FROM history").fetchone()[0])'), '123');
  } finally { writer.kill(); }
});

test('external research backup failure aborts the backup rather than claiming complete preservation', async () => {
  const item = fixture(); fs.mkdirSync(item.userDataPath); fs.writeFileSync(item.source, 'old');
  await assert.rejects(() => createUpdateBackup({ userDataPath: item.userDataPath, fromVersion: '1', toVersion: '2', researchDBPath: item.source, sqlitePython: '' }), /原数据库已保留/);
  assert.equal(fs.readFileSync(item.source, 'utf8'), 'old');
});

test('internal research backup snapshots an active WAL database without copying obsolete WAL or SHM', { skip: !pythonAvailable }, async () => {
  const item = fixture(); fs.mkdirSync(item.userDataPath);
  const source = path.join(item.userDataPath, 'nested', 'research.db');
  fs.mkdirSync(path.dirname(source));
  const { spawn } = require('node:child_process');
  const writer = spawn(python, ['-I', '-u', '-c', 'import sqlite3,sys,time\nc=sqlite3.connect(sys.argv[1])\nc.execute("PRAGMA journal_mode=WAL")\nc.execute("CREATE TABLE history(value)")\nc.execute("INSERT INTO history VALUES (456)")\nc.commit()\nprint("ready",flush=True)\nfor i in range(300):\n c.execute("INSERT INTO history VALUES (?)",(1000+i,))\n c.commit()\n time.sleep(0.05)', source], { stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
  try {
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('SQLite writer fixture did not become ready')), 5000);
      writer.stdout.once('data', () => { clearTimeout(timer); resolve(); });
      writer.once('error', error => { clearTimeout(timer); reject(error); });
      writer.once('exit', code => { if (code) { clearTimeout(timer); reject(new Error('SQLite fixture exited')); } });
    });
    assert.ok(fs.existsSync(`${source}-wal`)); assert.ok(fs.existsSync(`${source}-shm`));
    const backup = await createUpdateBackup({ userDataPath: item.userDataPath, fromVersion: '1', toVersion: '2', researchDBPath: source, sqlitePython: python });
    const relative = path.join('nested', 'research.db');
    const snapshot = path.join(backup.path, 'data', relative);
    assert.equal(sqlite(snapshot, 'print(c.execute("SELECT value FROM history WHERE value=456").fetchone()[0])\nprint(c.execute("PRAGMA integrity_check").fetchone()[0])'), '456\nok');
    assert.equal(fs.existsSync(`${snapshot}-wal`), false); assert.equal(fs.existsSync(`${snapshot}-shm`), false);
    assert.equal(backup.manifest.files.filter(file => file.path === relative).length, 1);
    assert.equal(backup.manifest.files.some(file => file.path === `${relative}-wal` || file.path === `${relative}-shm`), false);
    assert.equal(backup.manifest.researchSnapshotPath, relative); assert.equal(backup.manifest.researchDBPath, source);
    assert.ok(fs.existsSync(`${source}-wal`)); assert.ok(fs.existsSync(`${source}-shm`));
  } finally { writer.kill(); }
});
