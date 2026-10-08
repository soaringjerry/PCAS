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

/** The calendar date in the given zone, `days` from today, as YYYY-MM-DD. */
export function civilDate(days = 0, timeZone?: string): string {
  return new Date((calendarDay(new Date(), timeZone) + days) * DAY).toISOString().slice(0, 10)
}

/** Whole days from today in the given zone to a YYYY-MM-DD date. */
export function civilOffset(date: string, timeZone?: string): number {
  return Math.round(Date.parse(`${date}T00:00:00Z`) / DAY) - calendarDay(new Date(), timeZone)
}

/** "今天", "明天", "10月3日" for a YYYY-MM-DD date. */
export function formatCivil(date: string, timeZone?: string): string {
  const offset = civilOffset(date, timeZone)
  if (offset === 0) return '今天'
  if (offset === 1) return '明天'
  if (offset === -1) return '昨天'
  const [, month, day] = date.split('-').map(Number)
  return `${month}月${day}日`
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
export function formatAgo(iso: string, timeZone?: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  if (diff < 60 * 1000) return '刚刚'
  const minutes = Math.floor(diff / 60000)
  if (minutes < 60) return `${minutes} 分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days} 天前`
  return formatWhen(iso, timeZone)
}

export function isOverdue(iso: string): boolean {
  return new Date(iso).getTime() < Date.now()
}

/** Calendar fields and weekday in the workspace zone, never the browser zone. */
function dateParts(iso: string, timeZone?: string) {
  const parts = new Intl.DateTimeFormat('zh-CN', { timeZone, year: 'numeric', month: 'numeric', day: 'numeric', weekday: 'short' }).formatToParts(new Date(iso))
  const value = (type: string) => parts.find((p) => p.type === type)!.value
  return { year: value('year'), month: value('month'), day: value('day'), weekday: value('weekday').replace('周', '') }
}

function relativeDay(iso: string, timeZone?: string): string | undefined {
  const days = dayOffset(iso, timeZone)
  return days === 0 ? '今天' : days === 1 ? '明天' : days === -1 ? '昨天' : undefined
}

/** "今天", "3月12日", "2025年3月" — short enough for a margin. */
export function formatShortDate(iso: string, timeZone?: string): string {
  if (Number.isNaN(new Date(iso).getTime())) return ''
  const relative = relativeDay(iso, timeZone)
  if (relative) return relative
  const d = dateParts(iso, timeZone)
  return d.year === dateParts(nowIso(), timeZone).year ? `${d.month}月${d.day}日` : `${d.year}年${d.month}月`
}

/** "今天 15:00", "周五 15:00" within the week, otherwise "10月12日 15:00". */
export function formatShortWhen(iso: string, timeZone?: string): string {
  const relative = relativeDay(iso, timeZone)
  const time = clockTime(iso, timeZone)
  if (relative) return `${relative} ${time}`
  const d = dateParts(iso, timeZone)
  const days = dayOffset(iso, timeZone)
  if (days > 1 && days < 7) return `周${d.weekday} ${time}`
  const year = d.year === dateParts(nowIso(), timeZone).year ? '' : `${d.year}年`
  return `${year}${d.month}月${d.day}日 ${time}`
}

/** "今天 18:00", "10月3日 周六 14:30", "2027年1月2日 周六 09:00" */
export function formatDateTime(iso: string, timeZone?: string): string {
  const relative = relativeDay(iso, timeZone)
  const time = clockTime(iso, timeZone)
  if (relative) return `${relative} ${time}`
  const d = dateParts(iso, timeZone)
  const year = d.year === dateParts(nowIso(), timeZone).year ? '' : `${d.year}年`
  return `${year}${d.month}月${d.day}日 周${d.weekday} ${time}`
}

/** Preserve the browser's locale for full timestamps, with an explicit workspace zone. */
export function formatTimestamp(iso: string, timeZone?: string): string {
  return new Date(iso).toLocaleString(undefined, { timeZone })
}
