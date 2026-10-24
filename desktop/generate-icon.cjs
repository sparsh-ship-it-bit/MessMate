'use strict';
const fs = require('node:fs');
const path = require('node:path');
const sharp = require('sharp');
const pngToIco = require('png-to-ico');
const root = __dirname;
const assets = path.join(root, 'assets');
(async () => {
  fs.mkdirSync(assets, { recursive: true });
  const svg = fs.readFileSync(path.join(assets, 'messmate.svg'));
  const png = path.join(assets, 'messmate-icon.png');
  const ico = path.join(assets, 'messmate.ico');
  await sharp(svg).resize(256, 256).png().toFile(png);
  fs.writeFileSync(ico, await pngToIco([png]));
  console.log('Generated MessMate Windows icon:', ico);
})().catch(error => { console.error(error); process.exit(1); });
