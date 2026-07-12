// token 放 localStorage:刷新页面不用重输;后端为空 token 时不鉴权(开发模式),
// 前端无从预知,所以策略是"先裸跑,收到 401 再弹令牌输入"(见 App 的 TokenGate)。
const KEY = 'relay_api_token'

export function getToken(): string {
  return localStorage.getItem(KEY) ?? ''
}

export function setToken(token: string) {
  localStorage.setItem(KEY, token)
}

export function clearToken() {
  localStorage.removeItem(KEY)
}

// 有 token 才带头:空 Authorization 头会被后端当无效 token 拒掉
export function authHeaders(): Record<string, string> {
  const t = getToken()
  return t ? { Authorization: `Bearer ${t}` } : {}
}

// 401 广播:client 与 SSE 都可能先撞到 401,统一通知 App 层弹出令牌输入
type Listener = () => void
const listeners = new Set<Listener>()

export function onUnauthorized(fn: Listener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export function notifyUnauthorized() {
  for (const fn of listeners) fn()
}
