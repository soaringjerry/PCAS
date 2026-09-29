const DAY = 24 * 60 * 60 * 1000

export function nowIso(): string {
  return new Date().toISOString()
}

/** A time `days` ago (fractions allowed), used for seed data. */
export function ago(days: number): string {
  return new Date(Date.now() - days * DAY).toISOString()
}

/** A time `days` from now at the given local hour. */
export function ahead(days: number, hour = 9): string {
  const d = new Date(Date.now() + days * DAY)
  d.setHours(hour, 0, 0, 0)
  return d.toISOString()
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

/** Whole calendar days from today; negative is in the past. */
export function dayOffset(iso: string): number {
  return Math.round((startOfDay(new Date(iso)) - startOfDay(new Date())) / DAY)
}

function hhmm(d: Date): string {
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}

/** "今天 09:00", "明天 14:00", "10月3日" */
export function formatWhen(iso: string): string {
  const d = new Date(iso)
  const offset = dayOffset(iso)
  if (offset === 0) return `今天 ${hhmm(d)}`
  if (offset === 1) return `明天 ${hhmm(d)}`
  if (offset === -1) return `昨天 ${hhmm(d)}`
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

/** "刚刚", "3 小时前", "2 天前" */
export function formatAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  if (diff < 60 * 1000) return '刚刚'
  const minutes = Math.floor(diff / 60000)
  if (minutes < 60) return `${minutes} 分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days} 天前`
  return formatWhen(iso)
}

export function isOverdue(iso: string): boolean {
  return new Date(iso).getTime() < Date.now()
}

/** ISO -> value for <input type="datetime-local"> */
export function toLocalInput(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function fromLocalInput(value: string): string | undefined {
  return value ? new Date(value).toISOString() : undefined
}
