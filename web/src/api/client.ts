const jsonHeaders = { 'Content-Type': 'application/json' }

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  if (!res.ok) {
    // 后端统一错误结构 {"error": "..."},解析失败则退回状态码
    const body = await res.json().catch(() => null)
    throw new Error(body?.error ?? `HTTP ${res.status}`)
  }
  return res.json()
}

export const api = {
  listRuns: () => request<Run[]>('/api/runs'),
  getRun: (id: string) => request<Run>(`/api/runs/${id}`),
  createRun: (goal: string) =>
    request<Run>('/api/runs', {
      method: 'POST', headers: jsonHeaders, body: JSON.stringify({ goal }),
    }),
  cancelRun: (id: string) =>
    request<{ status: string }>(`/api/runs/${id}/cancel`, { method: 'POST' }),
}

import type { Run } from './types'