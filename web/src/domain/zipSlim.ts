/**
 * A ChatGPT export is a zip that can run to gigabytes, nearly all of it images
 * and audio. Only `conversations.json` is imported, so the browser takes that
 * one entry out and sends it alone. The zip is never read whole: its directory
 * sits at the end and says where each entry is.
 */

const view = async (file: Blob, from: number, to: number) => new DataView(await file.slice(from, to).arrayBuffer())

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

/**
 * The conversations file from a chat export zip, as a file of its own. Any
 * other file, or a zip this cannot read, comes back unchanged and is handled
 * by the server as before.
 */
export async function conversationsOnly(file: File): Promise<File> {
  if (!/\.zip$/i.test(file.name) && file.type !== 'application/zip') return file
  try {
    const entry = await findConversations(file)
    // Encrypted, or packed in a way the browser cannot unpack: leave it to the server.
    if (!entry || entry.flags & 1 || (entry.method !== 0 && entry.method !== 8)) return file
    const local = await view(file, entry.header, entry.header + 30)
    if (local.getUint32(0, true) !== 0x04034b50) return file
    const from = entry.header + 30 + local.getUint16(26, true) + local.getUint16(28, true)
    const packed = file.slice(from, from + entry.packed)
    if (packed.size !== entry.packed) return file
    const data = entry.method === 0 ? packed : await new Response(packed.stream().pipeThrough(new DecompressionStream('deflate-raw'))).blob()
    if (data.size !== entry.size) return file
    return new File([data], `${file.name.replace(/\.zip$/i, '')}.conversations.json`, { type: 'application/json', lastModified: file.lastModified })
  } catch {
    return file
  }
}
