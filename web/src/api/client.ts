import { authHeaders, notifyUnauthorized } from './auth'
import type { Run } from './types'

const jsonHeaders = { 'Content-Type': 'application/json' }

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: { ...authHeaders(), ...init?.headers },
  })
  if (res.status === 401) {
    // 广播给 TokenGate 弹令牌输入;仍然抛错让调用方(React Query)进入 error 态
    notifyUnauthorized()
    throw new Error('未授权:需要有效的访问令牌')
  }
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
  decideApproval: (id: string, toolCallId: string, approved: boolean) =>
    request<{ status: string }>(`/api/runs/${id}/approval`, {
      method: 'POST',
      headers: jsonHeaders,
      body: JSON.stringify({ tool_call_id: toolCallId, approved }),
    }),
}
