/**
 * A ChatGPT export is a zip that can run to gigabytes, nearly all of it images
 * and audio. Only `conversations.json` is imported, so the browser takes that
 * one entry out and sends it alone. The zip is never read whole: its directory
 * sits at the end and says where each entry is.
 */

import { Inflate } from 'fflate'

const view = async (file: Blob, from: number, to: number) => new DataView(await file.slice(from, to).arrayBuffer())

/** ZIP uses raw DEFLATE, which some browsers do not support even though they
 * expose DecompressionStream. Fall back to a bundled decoder, still reading
 * only this entry. Its output must fit the size declared by the ZIP directory.
 */
async function inflateEntry(packed: Blob, expectedSize: number): Promise<Blob> {
  if (typeof DecompressionStream !== 'undefined') {
    try {
      return await new Response(packed.stream().pipeThrough(new DecompressionStream('deflate-raw'))).blob()
    } catch { /* Try the same entry with the portable decoder. */ }
  }
  const parts: BlobPart[] = []
  let size = 0
  const inflater = new Inflate((chunk) => {
    size += chunk.byteLength
    if (size > expectedSize) throw new Error('zip entry exceeds declared size')
    // Keep owned buffers; the decoder reuses its rolling window.
    parts.push(new Uint8Array(chunk).buffer)
  })
  const reader = packed.stream().getReader()
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      inflater.push(value)
    }
    inflater.push(new Uint8Array(0), true)
  } finally {
    await reader.cancel()
    reader.releaseLock()
  }
  return new Blob(parts)
}

/** A 64-bit little-endian size or offset; anything past what a number holds exactly is not a file we can slice. */
function u64(v: DataView, at: number): number {
  const n = v.getUint32(at, true) + v.getUint32(at + 4, true) * 2 ** 32
  if (!Number.isSafeInteger(n)) throw new Error('zip offset out of range')
  return n
}

interface Entry { method: number; flags: number; packed: number; size: number; header: number }

/** Finds the conversations file in the zip's directory, or nothing if this is not such a zip. */
async function findConversations(file: File): Promise<Entry | undefined> {
  // The end record is the last 22 bytes plus an optional comment of up to 64 KiB.
  const tailFrom = Math.max(0, file.size - 65_557)
  const tail = await view(file, tailFrom, file.size)
  let end = -1
  for (let i = tail.byteLength - 22; i >= 0; i--) if (tail.getUint32(i, true) === 0x06054b50) { end = i; break }
  if (end < 0) return undefined
  let dirSize = tail.getUint32(end + 12, true)
  let dirFrom = tail.getUint32(end + 16, true)
  if (dirSize === 0xffffffff || dirFrom === 0xffffffff || tail.getUint16(end + 10, true) === 0xffff) {
    // Zip64: a locator just before the end record points at the real sizes.
    const locatorAt = tailFrom + end - 20
    if (locatorAt < 0) return undefined
    const locator = await view(file, locatorAt, locatorAt + 20)
    if (locator.getUint32(0, true) !== 0x07064b50) return undefined
    const recordAt = u64(locator, 8)
    const record = await view(file, recordAt, recordAt + 56)
    if (record.getUint32(0, true) !== 0x06064b50) return undefined
    dirSize = u64(record, 40)
    dirFrom = u64(record, 48)
  }
  if (dirSize > 256 * 1024 * 1024 || dirFrom + dirSize > file.size) return undefined
  const dir = await view(file, dirFrom, dirFrom + dirSize)
  const text = new TextDecoder()
  for (let at = 0; at + 46 <= dir.byteLength && dir.getUint32(at, true) === 0x02014b50; ) {
    const nameLength = dir.getUint16(at + 28, true)
    const extraLength = dir.getUint16(at + 30, true)
    const commentLength = dir.getUint16(at + 32, true)
    const name = text.decode(new Uint8Array(dir.buffer, dir.byteOffset + at + 46, nameLength))
    if (name.split('/').pop() === 'conversations.json') {
      const entry: Entry = { flags: dir.getUint16(at + 8, true), method: dir.getUint16(at + 10, true), packed: dir.getUint32(at + 20, true), size: dir.getUint32(at + 24, true), header: dir.getUint32(at + 42, true) }
      // Zip64 keeps whichever of these overflowed in an extra field, in this order.
      const extraFrom = at + 46 + nameLength
      for (let x = extraFrom; x + 4 <= extraFrom + extraLength; ) {
        const id = dir.getUint16(x, true)
        const length = dir.getUint16(x + 2, true)
        if (id === 0x0001) {
          let p = x + 4
          if (entry.size === 0xffffffff) { entry.size = u64(dir, p); p += 8 }
          if (entry.packed === 0xffffffff) { entry.packed = u64(dir, p); p += 8 }
          if (entry.header === 0xffffffff) entry.header = u64(dir, p)
        }
        x += 4 + length
      }
      return entry
    }
    at += 46 + nameLength + extraLength + commentLength
  }
  return undefined
}

/** Why a zip is sent whole instead of only its conversations file, in words for the user. */
export type KeptWhole = '' | '压缩包里没有找到 conversations.json' | '压缩包加了密' | '压缩包用了浏览器解不开的压缩方式' | '压缩包读不出来，可能下载时损坏了'

/** What is sent for a picked file, and why when it is still the whole zip. */
export interface Slimmed { file: File; whole: KeptWhole }

/**
 * The conversations file from a chat export zip, as a file of its own. Any
 * other file comes back unchanged; a zip this cannot take apart comes back
 * unchanged with the reason, and is handled by the server as before.
 */
export async function conversationsOnly(file: File): Promise<Slimmed> {
  if (!/\.zip$/i.test(file.name) && file.type !== 'application/zip') return { file, whole: '' }
  try {
    const entry = await findConversations(file)
    if (!entry) return { file, whole: '压缩包里没有找到 conversations.json' }
    if (entry.flags & 1) return { file, whole: '压缩包加了密' }
    if (entry.method !== 0 && entry.method !== 8) return { file, whole: '压缩包用了浏览器解不开的压缩方式' }
    const local = await view(file, entry.header, entry.header + 30)
    if (local.getUint32(0, true) !== 0x04034b50) return { file, whole: '压缩包读不出来，可能下载时损坏了' }
    const from = entry.header + 30 + local.getUint16(26, true) + local.getUint16(28, true)
    const packed = file.slice(from, from + entry.packed)
    if (packed.size !== entry.packed) return { file, whole: '压缩包读不出来，可能下载时损坏了' }
    const data = entry.method === 0 ? packed : await inflateEntry(packed, entry.size)
    if (data.size !== entry.size) return { file, whole: '压缩包读不出来，可能下载时损坏了' }
    return { file: new File([data], `${file.name.replace(/\.zip$/i, '')}.conversations.json`, { type: 'application/json', lastModified: file.lastModified }), whole: '' }
  } catch {
    return { file, whole: '压缩包读不出来，可能下载时损坏了' }
  }
}
