// Generates the macOS menu-bar (tray) icons from the same 5-bar equalizer glyph
// as resources/icon.svg, so the tray and the app icon can't drift apart.
//
// Why generated and not a checked-in binary: the geometry below IS the spec, it
// stays reviewable in a diff, and regenerating at a new size is one edit.
//
// Two variants, both 1x + @2x (macOS picks the @2x file automatically when it
// sits next to the 1x one):
//   trayTemplate.png  black + alpha, a TEMPLATE image — macOS tints it itself, so
//                     it stays legible in a light or dark menu bar. Never colored.
//   trayActive.png    system red, NOT a template, shown while dictating. A template
//                     image can't be red, which is exactly why this one is separate.
//
// Run: npm run build:tray

import { deflateSync } from "node:zlib";
import { writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const OUT = join(dirname(fileURLToPath(import.meta.url)), "..", "resources");

// Bar heights as a fraction of the glyph box, measured off resources/icon.svg.
const BARS = [0.3, 0.66, 1.0, 0.66, 0.3];
const SS = 8; // supersampling factor -> anti-aliased edges

/** Coverage mask (0..1 per pixel) of the 5 rounded bars inside a size×size box. */
function renderMask(size) {
  const pad = Math.max(1, Math.round(size * 0.06));
  const box = size - pad * 2;
  // 5 bars + 4 gaps; a gap is half a bar wide, matching the source artwork.
  const barW = box / (BARS.length + (BARS.length - 1) * 0.5);
  const gap = barW * 0.5;
  const r = barW / 2; // fully rounded ends, like the app icon

  const W = size * SS;
  const acc = new Float64Array(size * size);

  for (let i = 0; i < BARS.length; i++) {
    const x0 = (pad + i * (barW + gap)) * SS;
    const x1 = x0 + barW * SS;
    const h = box * BARS[i] * SS;
    const y0 = (size / 2) * SS - h / 2;
    const y1 = y0 + h;
    const rr = r * SS;

    const px0 = Math.max(0, Math.floor(x0)), px1 = Math.min(W, Math.ceil(x1));
    const py0 = Math.max(0, Math.floor(y0)), py1 = Math.min(W, Math.ceil(y1));
    for (let py = py0; py < py1; py++) {
      for (let px = px0; px < px1; px++) {
        const cx = px + 0.5, cy = py + 0.5;
        if (cx < x0 || cx > x1 || cy < y0 || cy > y1) continue;
        // Rounded caps: outside the end circles doesn't count.
        let inside = true;
        if (cy < y0 + rr) inside = (cx - (x0 + rr)) ** 2 + (cy - (y0 + rr)) ** 2 <= rr * rr
          || cx >= x0 + rr && cx <= x1 - rr;
        else if (cy > y1 - rr) inside = (cx - (x0 + rr)) ** 2 + (cy - (y1 - rr)) ** 2 <= rr * rr
          || cx >= x0 + rr && cx <= x1 - rr;
        if (inside && cy < y0 + rr && cx > x1 - rr) {
          inside = (cx - (x1 - rr)) ** 2 + (cy - (y0 + rr)) ** 2 <= rr * rr;
        }
        if (inside && cy > y1 - rr && cx > x1 - rr) {
          inside = (cx - (x1 - rr)) ** 2 + (cy - (y1 - rr)) ** 2 <= rr * rr;
        }
        if (inside) acc[Math.floor(py / SS) * size + Math.floor(px / SS)] += 1;
      }
    }
  }
  const per = SS * SS;
  return acc.map((v) => Math.min(1, v / per));
}

function png(size, mask, [r, g, b]) {
  const raw = Buffer.alloc(size * (size * 4 + 1));
  let o = 0;
  for (let y = 0; y < size; y++) {
    raw[o++] = 0; // filter: none
    for (let x = 0; x < size; x++) {
      const a = Math.round(mask[y * size + x] * 255);
      raw[o++] = r; raw[o++] = g; raw[o++] = b; raw[o++] = a;
    }
  }
  const chunk = (type, data) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(td) >>> 0);
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; ihdr[9] = 6; // 8-bit RGBA
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

let TABLE;
function crc32(buf) {
  if (!TABLE) {
    TABLE = new Int32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      TABLE[n] = c;
    }
  }
  let c = -1;
  for (const b of buf) c = TABLE[(c ^ b) & 0xff] ^ (c >>> 8);
  return c ^ -1;
}

mkdirSync(OUT, { recursive: true });
const RED = [255, 59, 48]; // macOS system red
for (const [name, color] of [["trayTemplate", [0, 0, 0]], ["trayActive", RED]]) {
  for (const [size, suffix] of [[16, ""], [32, "@2x"]]) {
    const file = join(OUT, `${name}${suffix}.png`);
    writeFileSync(file, png(size, renderMask(size), color));
    console.log(`wrote ${file} (${size}x${size})`);
  }
}
