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

// Map zoned calendar dates onto UTC day numbers; DST days may be 23 or 25 hours.
function calendarDay(d: Date, timeZone?: string): number {
  const parts = new Intl.DateTimeFormat('en', { timeZone, year: 'numeric', month: 'numeric', day: 'numeric' }).formatToParts(d)
  const n = (type: string) => Number(parts.find((p) => p.type === type)!.value)
  return Date.UTC(n('year'), n('month') - 1, n('day')) / DAY
}

/** Whole calendar days from today in the given zone; negative is in the past. */
export function dayOffset(iso: string, timeZone?: string): number {
  return calendarDay(new Date(iso), timeZone) - calendarDay(new Date(), timeZone)
}

export function clockTime(iso: string, timeZone?: string): string {
  return new Date(iso).toLocaleTimeString('zh-CN', { timeZone, hour: '2-digit', minute: '2-digit', hour12: false })
}

/** "今天 09:00", "明天 14:00", "10月3日" */
export function formatWhen(iso: string, timeZone?: string): string {
  const offset = dayOffset(iso, timeZone)
  if (offset === 0) return `今天 ${clockTime(iso, timeZone)}`
  if (offset === 1) return `明天 ${clockTime(iso, timeZone)}`
  if (offset === -1) return `昨天 ${clockTime(iso, timeZone)}`
  return new Date(iso).toLocaleDateString('zh-CN', { timeZone, month: 'long', day: 'numeric' })
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

const weekdays = ['一', '二', '三', '四', '五', '六', '日']
const pad = (n: number) => String(n).padStart(2, '0')

/** "今天 18:00", "明天 09:00", "10月3日 周六 14:30", "2027年1月2日 周六 09:00" */
export function formatDateTime(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  const days = dayOffset(iso)
  if (days === 0) return `今天 ${time}`
  if (days === 1) return `明天 ${time}`
  if (days === -1) return `昨天 ${time}`
  const year = d.getFullYear() === now.getFullYear() ? '' : `${d.getFullYear()}年`
  return `${year}${d.getMonth() + 1}月${d.getDate()}日 周${weekdays[(d.getDay() + 6) % 7]} ${time}`
}
