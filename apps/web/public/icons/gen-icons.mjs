// One-off placeholder PWA icon generator.
//
// Produces solid theme-color square PNGs (electric blue #3b82f6, matching the
// app's --color-accent) with a white "F" initial. These are intentionally
// simple placeholders for installability; replace with designed artwork later.
//
// Run from this directory: `node gen-icons.mjs`
import { deflateSync } from 'node:zlib';
import { writeFileSync } from 'node:fs';

// Theme color (electric blue) and white foreground.
const BG = [0x3b, 0x82, 0xf6];
const FG = [0xff, 0xff, 0xff];

// 5x7 bitmap for the letter "F".
const F = ['11111', '10000', '10000', '11110', '10000', '10000', '10000'];

function crc32(buf) {
  let c = ~0;
  for (let i = 0; i < buf.length; i++) {
    c ^= buf[i];
    for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
  }
  return ~c >>> 0;
}

function chunk(type, data) {
  const typeBuf = Buffer.from(type, 'ascii');
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length, 0);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([typeBuf, data])), 0);
  return Buffer.concat([len, typeBuf, data, crc]);
}

function makePng(size) {
  // RGBA raster, one filter byte (0) per row.
  const stride = size * 4 + 1;
  const raw = Buffer.alloc(stride * size);

  // Glyph geometry: center a 5x7 grid occupying ~50% of the canvas.
  const cell = Math.floor(size / 12);
  const glyphW = cell * 5;
  const glyphH = cell * 7;
  const offX = Math.floor((size - glyphW) / 2);
  const offY = Math.floor((size - glyphH) / 2);

  for (let y = 0; y < size; y++) {
    raw[y * stride] = 0; // filter type: none
    for (let x = 0; x < size; x++) {
      let color = BG;
      const gx = Math.floor((x - offX) / cell);
      const gy = Math.floor((y - offY) / cell);
      if (gx >= 0 && gx < 5 && gy >= 0 && gy < 7 && F[gy][gx] === '1') {
        color = FG;
      }
      const p = y * stride + 1 + x * 4;
      raw[p] = color[0];
      raw[p + 1] = color[1];
      raw[p + 2] = color[2];
      raw[p + 3] = 0xff;
    }
  }

  const sig = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 6; // color type: RGBA
  ihdr[10] = 0;
  ihdr[11] = 0;
  ihdr[12] = 0;
  const idat = deflateSync(raw);
  return Buffer.concat([
    sig,
    chunk('IHDR', ihdr),
    chunk('IDAT', idat),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

for (const size of [192, 512]) {
  writeFileSync(new URL(`./icon-${size}.png`, import.meta.url), makePng(size));
  console.log(`wrote icon-${size}.png`);
}
