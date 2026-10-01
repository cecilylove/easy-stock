const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

function snapshotSQLite({ python, source, destination }) {
  if (!python) throw new Error('无法安全迁移/备份个股研究历史：SQLite Python 运行时不可用，原数据库已保留');
  const temporary = `${destination}.${process.pid}.${Date.now()}.tmp`;
  fs.mkdirSync(path.dirname(destination), { recursive: true, mode: 0o700 });
  const script = 'import sqlite3,sys,pathlib\nsrc=sqlite3.connect(pathlib.Path(sys.argv[1]).resolve().as_uri()+"?mode=ro",uri=True,timeout=10)\ndst=sqlite3.connect(sys.argv[2])\nsrc.backup(dst)\ndst.close()\nsrc.close()';
  try {
    const result = spawnSync(python, ['-I', '-c', script, source, temporary], { encoding: 'utf8', windowsHide: true, timeout: 30000 });
    if (result.error || result.status !== 0) throw new Error('无法安全迁移/备份个股研究历史，原数据库已保留；请关闭占用程序并重试');
    // Same-directory hard link publishes the complete snapshot atomically and
    // rejects an existing target; unlike a copy, disk errors cannot leave a
    // partial database that a later launch might mistake for migrated history.
    fs.chmodSync(temporary, 0o600);
    fs.linkSync(temporary, destination);
  } finally {
    fs.rmSync(temporary, { force: true });
    for (const suffix of ['-journal', '-wal', '-shm']) fs.rmSync(`${temporary}${suffix}`, { force: true });
  }
}

function resolveResearchData({ userDataPath, configDir, configuredPath = '', isolated = false, python }) {
  if (configuredPath) return { path: path.resolve(configuredPath), state: 'explicit' };
  const destination = path.join(userDataPath, 'stock-research.db');
  const current = path.join(configDir, 'easy-stock');
  const legacy = path.join(configDir, 'a-stock-ai');
  const defaultDirectory = fs.existsSync(path.join(current, 'settings.json')) ? current
    : fs.existsSync(path.join(legacy, 'settings.json')) ? legacy : current;
  const source = path.join(defaultDirectory, 'stock-research.db');
  const samePath = (a, b) => process.platform === 'win32' ? path.resolve(a).toLowerCase() === path.resolve(b).toLowerCase() : path.resolve(a) === path.resolve(b);
  if (samePath(source, destination) || !fs.existsSync(source)) return { path: destination, state: 'local' };
  fs.mkdirSync(userDataPath, { recursive: true, mode: 0o700 });
  const noticePath = path.join(userDataPath, 'stock-research-migration.json');
  let previous = '';
  try { previous = fs.readFileSync(noticePath, 'utf8'); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  try {
    const known = JSON.parse(previous);
    if (known.state === 'migrated' && known.source === source && known.destination === destination && fs.existsSync(destination)) {
      return { path: destination, state: 'local' };
    }
  } catch {}
  let state = fs.existsSync(destination) ? 'preserved-both' : isolated ? 'isolated-source-preserved' : 'migrated';
  if (state === 'migrated') {
    try { snapshotSQLite({ python, source, destination }); } catch (error) {
      // Continue using the intact original history, never silently start an empty DB.
      state = error.code === 'EEXIST' ? 'preserved-both' : 'migration-deferred';
    }
  }
  const notice = { state, source, destination, message: state === 'migrated' ? '研究历史已安全复制，原数据库仍保留'
    : state === 'migration-deferred' ? '研究历史暂未迁移，当前继续使用原数据库；请检查 SQLite Python 运行时，迁移提示已保存在应用数据目录'
    : '其他目录的研究历史仍保留，未自动导入或覆盖当前记录' };
  const serialized = `${JSON.stringify(notice, null, 2)}\n`;
  if (previous !== serialized) fs.writeFileSync(noticePath, serialized, { mode: 0o600 });
  return { path: state === 'migration-deferred' ? source : destination, notify: previous !== serialized, ...notice };
}

module.exports = { resolveResearchData, snapshotSQLite };
