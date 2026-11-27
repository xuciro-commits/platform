type VersionSpec = { version: number; size: number; dataBytes: number; ecBytes: number; blocks: number; align: number[] };

const versions: VersionSpec[] = [
  { version: 1, size: 21, dataBytes: 19, ecBytes: 7, blocks: 1, align: [] },
  { version: 2, size: 25, dataBytes: 34, ecBytes: 10, blocks: 1, align: [6, 18] },
  { version: 3, size: 29, dataBytes: 55, ecBytes: 15, blocks: 1, align: [6, 22] },
  { version: 4, size: 33, dataBytes: 80, ecBytes: 20, blocks: 1, align: [6, 26] },
  { version: 5, size: 37, dataBytes: 108, ecBytes: 26, blocks: 1, align: [6, 30] },
  { version: 6, size: 41, dataBytes: 136, ecBytes: 18, blocks: 2, align: [6, 34] },
];

const expTable = new Uint8Array(512);
const logTable = new Uint8Array(256);
let x = 1;
for (let i = 0; i < 255; i += 1) {
  expTable[i] = x;
  logTable[x] = i;
  x <<= 1;
  if (x & 0x100) x ^= 0x11d;
}
for (let i = 255; i < 512; i += 1) expTable[i] = expTable[i - 255];

function gfMul(a: number, b: number): number {
  return a === 0 || b === 0 ? 0 : expTable[logTable[a] + logTable[b]];
}

function rsRemainder(data: Uint8Array, ecLen: number): Uint8Array {
  let gen = new Uint8Array([1]);
  for (let i = 0; i < ecLen; i += 1) {
    const next = new Uint8Array(gen.length + 1);
    const factor = expTable[i];
    for (let j = 0; j < gen.length; j += 1) {
      next[j] ^= gen[j];
      next[j + 1] ^= gfMul(gen[j], factor);
    }
    gen = next;
  }
  const rem = new Uint8Array(ecLen);
  for (let i = 0; i < data.length; i += 1) {
    const factor = data[i] ^ rem[0];
    rem.copyWithin(0, 1);
    rem[ecLen - 1] = 0;
    for (let j = 0; j < ecLen; j += 1) rem[j] ^= gfMul(gen[j + 1], factor);
  }
  return rem;
}

function encodeMatrix(text: string): boolean[][] {
  const raw = new TextEncoder().encode(text);
  const spec = versions.find((v) => raw.length + 2 <= v.dataBytes) ?? versions[versions.length - 1];
  const bytes = raw.slice(0, spec.dataBytes - 2);
  const bits: number[] = [];
  const pushBits = (val: number, len: number) => {
    for (let i = len - 1; i >= 0; i -= 1) bits.push((val >> i) & 1);
  };
  pushBits(0b0100, 4);
  pushBits(bytes.length, 8);
  for (const byte of bytes) pushBits(byte, 8);
  pushBits(0, Math.min(4, spec.dataBytes * 8 - bits.length));
  while (bits.length % 8 !== 0) bits.push(0);
  const codewords = new Uint8Array(spec.dataBytes);
  for (let i = 0; i < bits.length / 8; i += 1) {
    let val = 0;
    for (let b = 0; b < 8; b += 1) val = (val << 1) | bits[i * 8 + b];
    codewords[i] = val;
  }
  let padIdx = 0;
  for (let i = bits.length / 8; i < spec.dataBytes; i += 1) {
    codewords[i] = padIdx % 2 === 0 ? 0xec : 0x11;
    padIdx += 1;
  }
  const blockSize = Math.floor(spec.dataBytes / spec.blocks);
  const dataBlocks: Uint8Array[] = [];
  const ecBlocks: Uint8Array[] = [];
  for (let b = 0; b < spec.blocks; b += 1) {
    const slice = codewords.subarray(b * blockSize, (b + 1) * blockSize);
    dataBlocks.push(slice);
    ecBlocks.push(rsRemainder(slice, spec.ecBytes));
  }
  const stream: number[] = [];
  for (let i = 0; i < blockSize; i += 1) {
    for (const block of dataBlocks) stream.push(block[i]);
  }
  for (let i = 0; i < spec.ecBytes; i += 1) {
    for (const block of ecBlocks) stream.push(block[i]);
  }

  const n = spec.size;
  const grid = Array.from({ length: n }, () => Array<boolean>(n).fill(false));
  const used = Array.from({ length: n }, () => Array<boolean>(n).fill(false));
  const set = (r: number, c: number, dark: boolean) => {
    if (r < 0 || c < 0 || r >= n || c >= n) return;
    grid[r][c] = dark;
    used[r][c] = true;
  };
  const finder = (top: number, left: number) => {
    for (let dr = -1; dr <= 7; dr += 1) {
      for (let dc = -1; dc <= 7; dc += 1) {
        const dark = dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6 &&
          (dr === 0 || dr === 6 || dc === 0 || dc === 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4));
        set(top + dr, left + dc, dark);
      }
    }
  };
  finder(0, 0);
  finder(0, n - 7);
  finder(n - 7, 0);
  for (let i = 8; i < n - 8; i += 1) {
    set(6, i, i % 2 === 0);
    set(i, 6, i % 2 === 0);
  }
  for (const r of spec.align) {
    for (const c of spec.align) {
      if (used[r][c]) continue;
      for (let dr = -2; dr <= 2; dr += 1) {
        for (let dc = -2; dc <= 2; dc += 1) {
          set(r + dr, c + dc, Math.max(Math.abs(dr), Math.abs(dc)) !== 1);
        }
      }
    }
  }
  const format = 0x77c4;
  for (let i = 0; i <= 5; i += 1) set(8, i, Boolean((format >> (14 - i)) & 1));
  set(8, 7, Boolean((format >> 8) & 1));
  set(8, 8, Boolean((format >> 7) & 1));
  set(7, 8, Boolean((format >> 6) & 1));
  for (let i = 0; i <= 5; i += 1) set(5 - i, 8, Boolean((format >> (5 - i)) & 1));
  for (let i = 0; i <= 7; i += 1) set(8, n - 1 - i, Boolean((format >> i) & 1));
  for (let i = 0; i <= 6; i += 1) set(n - 7 + i, 8, Boolean((format >> (14 - i)) & 1));
  set(n - 8, 8, true);

  let bitPos = 0;
  for (let col = n - 1; col >= 1; col -= 2) {
    const right = col === 6 ? col - 1 : col;
    const upward = ((n - 1 - right) >> 1) % 2 === 0;
    for (let step = 0; step < n; step += 1) {
      const r = upward ? n - 1 - step : step;
      for (const c of [right, right - 1]) {
        if (used[r][c]) continue;
        const byteIdx = bitPos >> 3;
        const bit = byteIdx < stream.length ? ((stream[byteIdx] >> (7 - (bitPos & 7))) & 1) === 1 : false;
        grid[r][c] = (r + c) % 2 === 0 ? !bit : bit;
        bitPos += 1;
      }
    }
  }
  return grid;
}

export function qrDataUri(text: string): string {
  const matrix = encodeMatrix(text);
  const quiet = 2;
  const total = matrix.length + quiet * 2;
  const parts: string[] = [];
  for (let r = 0; r < matrix.length; r += 1) {
    for (let c = 0; c < matrix.length; c += 1) {
      if (matrix[r][c]) parts.push(`M${c + quiet} ${r + quiet}h1v1h-1z`);
    }
  }
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${total} ${total}" shape-rendering="crispEdges"><rect width="${total}" height="${total}" fill="white"/><path d="${parts.join("")}" fill="black"/></svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}
