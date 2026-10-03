import { APIError } from '../store/api'
import type { DeskTurnRequest } from './desk'

const mediaByExtension: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', webp: 'image/webp', tif: 'image/tiff', tiff: 'image/tiff',
  pdf: 'application/pdf', mp3: 'audio/mpeg', wav: 'audio/wav', m4a: 'audio/mp4', ogg: 'audio/ogg',
}
export const attachmentAccept = Object.keys(mediaByExtension).map((extension) => `.${extension}`).join(',')
export interface SelectedAttachment { file: File; externalId: string }
function media(file: File) {
  // Browser MIME aliases vary; extensions match the existing import formats.
  return mediaByExtension[file.name.split('.').pop()?.toLowerCase() ?? ''] ?? (Object.values(mediaByExtension).includes(file.type) ? file.type : '')
}
export function checkAttachment(file: File): string {
  if (file.size > 20 * 1024 * 1024) return '附件超过 20 MB，请缩小后再发。'
  if (!file.size) return '附件是空的，请重新选择。'
  if (!media(file)) return '暂不支持这种文件，请选择图片、PDF 或音频。'
  return ''
}
export async function uploadSecretaryAttachment(selected: SelectedAttachment): Promise<NonNullable<DeskTurnRequest['attachments']>[number]> {
  const data = new FormData()
  data.append('file', new Blob([selected.file], { type: media(selected.file) }), selected.file.name)
  data.append('external_id', selected.externalId)
  const response = await fetch('/v1/memory/attachments', { method: 'POST', credentials: 'same-origin', body: data })
  const result = await response.json().catch(() => ({}))
  if (!response.ok) throw new APIError(response.status, response.status === 413 ? '附件超过 20 MB，请缩小后再发。' : '附件没传好，请检查文件格式后重试。', result.error)
  return { id: result.id, version: result.version, kind: 'source' }
}
