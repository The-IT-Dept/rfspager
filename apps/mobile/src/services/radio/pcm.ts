// Decoding /radio/live chunks: base64 of 8 kHz mono signed 16-bit
// little-endian PCM. Decoded straight from the base64 text into samples, so it
// needs neither atob nor Buffer and makes no intermediate binary string.

const LOOKUP = new Uint8Array(128).fill(255)
const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
for (let i = 0; i < ALPHABET.length; i++) LOOKUP[ALPHABET.charCodeAt(i)] = i
// URL-safe variants, in case a proxy ever rewrites them.
LOOKUP["-".charCodeAt(0)] = 62
LOOKUP["_".charCodeAt(0)] = 63

/** base64 → bytes. Skips padding, whitespace and anything else outside the alphabet. */
export function base64ToBytes(b64: string): Uint8Array {
  const out = new Uint8Array(Math.floor((b64.length * 3) / 4))
  let n = 0
  let acc = 0
  let bits = 0
  for (let i = 0; i < b64.length; i++) {
    const c = b64.charCodeAt(i)
    const v = c < 128 ? LOOKUP[c] : 255
    if (v === 255) continue
    acc = ((acc << 6) | v) & 0xffffff
    bits += 6
    if (bits >= 8) {
      bits -= 8
      out[n++] = (acc >> bits) & 0xff
    }
  }
  return n === out.length ? out : out.subarray(0, n)
}

/** base64 s16le → float samples in [-1, 1). */
export function decodePCM(b64: string): Float32Array<ArrayBuffer> {
  const bytes = base64ToBytes(b64)
  const out = new Float32Array(new ArrayBuffer((bytes.length >> 1) * 4))
  for (let i = 0; i < out.length; i++) {
    let v = bytes[2 * i] | (bytes[2 * i + 1] << 8)
    if (v & 0x8000) v -= 0x10000
    out[i] = v / 32768
  }
  return out
}
