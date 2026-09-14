#!/usr/bin/env node
// 安裝時抓下對應平台的 release 二進位。
//
// 為什麼走這條路而不是把六個平台的檔案都塞進 npm 套件：
// 那樣每個使用者都要下載約 30 MB 才拿到自己需要的那 4 MB。
//
// 為什麼要有 npm 這一層：`npx svgcast` 是最低摩擦的試用方式，
// 而這個工具的目標使用者（寫 README 的開源作者）多半手邊就有 Node。

const fs = require('fs');
const path = require('path');
const https = require('https');
const { execFileSync } = require('child_process');

const pkg = require('./package.json');
const REPO = 'co2water/svgcast';

const PLATFORMS = {
  'linux-x64': ['linux', 'amd64', 'tar.gz'],
  'linux-arm64': ['linux', 'arm64', 'tar.gz'],
  'darwin-x64': ['darwin', 'amd64', 'tar.gz'],
  'darwin-arm64': ['darwin', 'arm64', 'tar.gz'],
  'win32-x64': ['windows', 'amd64', 'zip'],
  'win32-arm64': ['windows', 'arm64', 'zip'],
};

function fail(msg) {
  console.error('svgcast: ' + msg);
  console.error('  Alternative install methods:');
  console.error('    go install github.com/' + REPO + '/cmd/svgcast@latest');
  console.error('    or download a release from https://github.com/' + REPO + '/releases');
  process.exit(1);
}

const key = `${process.platform}-${process.arch}`;
const target = PLATFORMS[key];
if (!target) fail(`no prebuilt binary for ${key}`);

const [goos, goarch, ext] = target;
const version = pkg.version;
const asset = `svgcast_${version}_${goos}_${goarch}.${ext}`;
const url = `https://github.com/${REPO}/releases/download/v${version}/${asset}`;

const binDir = path.join(__dirname, 'bin');
fs.mkdirSync(binDir, { recursive: true });
const binName = goos === 'windows' ? 'svgcast.exe' : 'svgcast';
const binPath = path.join(binDir, binName);

// Windows 上不能直接叫 `tar`：npm 用 cmd.exe 跑腳本，裝了 Git for Windows 的機器
// PATH 上排在前面的是 GNU tar，它不會解 zip——第一次本機測試就是這樣炸的。
// 能解 zip 的是 System32 的 bsdtar（Win10 1803 起內建），所以用絕對路徑；
// 再不行就退到 PowerShell 的 Expand-Archive（PowerShell 5 起內建）。
function extractZipWindows(zipPath, destDir) {
  const sysTar = path.join(process.env.SystemRoot || 'C:\\Windows', 'System32', 'tar.exe');
  try {
    execFileSync(sysTar, ['-xf', zipPath, '-C', destDir], { stdio: 'ignore' });
    return;
  } catch (_) { /* fall through */ }
  execFileSync('powershell.exe', [
    '-NoProfile', '-NonInteractive', '-Command',
    `Expand-Archive -LiteralPath '${zipPath.replace(/'/g, "''")}' -DestinationPath '${destDir.replace(/'/g, "''")}' -Force`,
  ], { stdio: 'ignore' });
}

function get(u, cb, depth = 0) {
  if (depth > 5) return fail('too many redirects');
  https.get(u, { headers: { 'User-Agent': 'svgcast-installer' } }, (res) => {
    if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
      res.resume();
      return get(res.headers.location, cb, depth + 1);
    }
    if (res.statusCode !== 200) {
      res.resume();
      return fail(`download of ${asset} failed (HTTP ${res.statusCode})`);
    }
    cb(res);
  }).on('error', (e) => fail('download failed: ' + e.message));
}

get(url, (res) => {
  const tmp = path.join(binDir, asset);
  const out = fs.createWriteStream(tmp);
  res.pipe(out);
  out.on('finish', () => {
    out.close(() => {
      try {
        if (ext === 'zip') {
          extractZipWindows(tmp, binDir);
        } else {
          execFileSync('tar', ['-xzf', tmp, '-C', binDir], { stdio: 'ignore' });
        }
        fs.unlinkSync(tmp);
        if (goos !== 'windows') fs.chmodSync(binPath, 0o755);
        if (!fs.existsSync(binPath)) fail('archive did not contain the expected binary');
      } catch (e) {
        fail('extract failed: ' + e.message);
      }
    });
  });
});
