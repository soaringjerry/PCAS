const common = [
  ['Australia/Melbourne', '墨尔本'], ['Australia/Sydney', '悉尼'],
  ['Asia/Shanghai', '上海 / 北京'], ['Asia/Hong_Kong', '香港'],
  ['Asia/Tokyo', '东京'], ['Asia/Singapore', '新加坡'],
  ['Europe/London', '伦敦'], ['Europe/Paris', '巴黎'],
  ['America/New_York', '纽约'], ['America/Los_Angeles', '洛杉矶'], ['UTC', '协调世界时'],
] as const

export function browserTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

/** Compare rules, so equivalent IANA aliases do not produce a false mismatch. */
export function sameTimezone(a: string, b: string): boolean {
  try {
    const canonical = (timeZone: string) => new Intl.DateTimeFormat('en', { timeZone }).resolvedOptions().timeZone
    return canonical(a) === canonical(b)
  } catch {
    return a === b
  }
}

export function timezoneOptions(current: string) {
  const zones = Intl.supportedValuesOf('timeZone')
  const names = new Map<string, string>(common)
  return [...new Set([...names.keys(), current, ...zones])].filter(Boolean).map((zone) => ({
    value: zone, label: names.has(zone) ? `${names.get(zone)} · ${zone}` : zone,
  }))
}
