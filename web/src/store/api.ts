export class APIError extends Error {
  status: number
  /** The server's error code, e.g. `changed_since`. */
  code?: string
  constructor(status: number, message: string, code?: string) { super(message); this.status = status; this.code = code }
}
const messages: Record<string, string> = {
  unauthorized: '请先登录 PCAS', version_conflict: '数据已在其他窗口或后台更新。已刷新，请检查后重试。',
  daily_budget_exceeded: '超过每日预算，可在设置中调整。', capability_not_configured: '服务尚未配置或登录，请检查设置。',
  invalid_input: '内容或状态不符合要求，请检查输入。', reimport_blocked: '这份内容已删除并阻止重新导入。',
  forbidden: '没有此操作的权限。', not_found: '记录已不存在，请刷新后重试。',
  changed_since: '这件事之后又改过，没法直接撤销。', work_started: '副手已经开始做了，没法撤销。', already_undone: '已经撤销过了。',
}
export async function api<T>(path: string, body?: unknown, method?: string): Promise<T> {
  const response = await fetch(path, {
    method: method ?? (body === undefined ? 'GET' : 'POST'), credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body, (_key, value) => value === undefined ? null : value),
  })
  const value = await response.json().catch(() => ({}))
  if (!response.ok) throw new APIError(response.status, messages[value.error] ?? (value.error === 'chatgpt_provider_error' ? value.message : undefined) ?? '服务暂时不可用，操作没有确认保存。', typeof value.error === 'string' ? value.error : undefined)
  return value as T
}
export async function downloadExport(training = false, confirmedOnly = false) {
  const response = await fetch(`/v1/workspace/export?training=${training}&confirmedOnly=${confirmedOnly}`, { credentials: 'same-origin' })
  if (!response.ok) throw new Error('导出失败，请重新登录后重试')
  const url = URL.createObjectURL(await response.blob())
  const a = document.createElement('a'); a.href = url
  a.download = training ? 'pcas-training.jsonl' : 'pcas-export.json'
  a.click(); URL.revokeObjectURL(url)
}
