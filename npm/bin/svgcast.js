#!/usr/bin/env node
// 把呼叫轉給 postinstall 抓下來的原生二進位。
const { spawnSync } = require('child_process');
const path = require('path');
const fs = require('fs');

const bin = path.join(__dirname, process.platform === 'win32' ? 'svgcast.exe' : 'svgcast');

if (!fs.existsSync(bin)) {
  console.error('svgcast: native binary not found; try `npm rebuild svgcast`');
  process.exit(1);
}

const r = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
process.exit(r.status === null ? 1 : r.status);
