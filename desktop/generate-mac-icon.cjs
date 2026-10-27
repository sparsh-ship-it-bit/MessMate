'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const sharp = require('sharp');

const assets = path.join(__dirname, 'assets');
const svgPath = path.join(assets, 'messmate.svg');
const iconset = path.join(assets, 'messmate.iconset');
const icnsPath = path.join(assets, 'messmate.icns');

const sizes = [
  ['icon_16x16.png', 16],
  ['icon_16x16@2x.png', 32],
  ['icon_32x32.png', 32],
  ['icon_32x32@2x.png', 64],
  ['icon_128x128.png', 128],
  ['icon_128x128@2x.png', 256],
  ['icon_256x256.png', 256],
  ['icon_256x256@2x.png', 512],
  ['icon_512x512.png', 512],
  ['icon_512x512@2x.png', 1024]
];

(async () => {
  if (process.platform !== 'darwin') {
    throw new Error('macOS icon generation must run on a macOS runner.');
  }
  fs.rmSync(iconset, { recursive: true, force: true });
  fs.mkdirSync(iconset, { recursive: true });
  const svg = fs.readFileSync(svgPath);
  for (const [name, size] of sizes) {
    await sharp(svg).resize(size, size).png().toFile(path.join(iconset, name));
  }
  execFileSync('iconutil', ['-c', 'icns', iconset, '-o', icnsPath], { stdio: 'inherit' });
  fs.rmSync(iconset, { recursive: true, force: true });
  console.log('Generated MessMate macOS icon:', icnsPath);
})().catch(error => {
  console.error(error);
  process.exit(1);
});
