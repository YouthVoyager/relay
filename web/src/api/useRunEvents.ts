import { useEffect, useRef, useState } from 'react'
import { authHeaders, notifyUnauthorized } from './auth'
import type { RunEvent } from './types'

// 不用 EventSource:它无法携带 Authorization 头,token 走 URL 又会进后端访问日志。
// 改用 fetch 读流,自己实现 EventSource 的两个关键行为:断线重连、重连带 Last-Event-ID。
export function useRunEvents(runId: string, onFinished?: () => void) {
  const [events, setEvents] = useState<RunEvent[]>([])
  const [connected, setConnected] = useState(false)
  // onFinished 存进 ref:避免它进依赖数组导致重连(见下文解释)
  const onFinishedRef = useRef(onFinished)
  onFinishedRef.current = onFinished

  useEffect(() => {
    // runId 变化时重置并重建连接
    setEvents([])
    const ctrl = new AbortController()
    let lastEventId = 0

    const handleEvent = (ev: RunEvent) => {
      setEvents((prev) => {
        // 去重:重连补发可能与已收到的重叠(镜像后端 lastSent 逻辑)
        if (prev.length > 0 && ev.id <= prev[prev.length - 1].id) return prev
        return [...prev, ev]
      })
      if (ev.type === 'run_finished') onFinishedRef.current?.()
    }

    const run = async () => {
      while (!ctrl.signal.aborted) {
        try {
          const res = await fetch(`/api/runs/${runId}/events/stream`, {
            signal: ctrl.signal,
            headers: {
              Accept: 'text/event-stream',
              ...authHeaders(),
              // 重连时从上次收到的位置续传,后端据此补发历史
              ...(lastEventId > 0 ? { 'Last-Event-ID': String(lastEventId) } : {}),
            },
          })
          if (res.status === 401) {
            // 交给 TokenGate;换 token 后组件重挂会重新建连,这里不再自行重试
            notifyUnauthorized()
            return
          }
          if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`)
          setConnected(true)
          await readSSE(res.body, (msg) => {
            lastEventId = msg.id
            if (msg.event === 'run_event') handleEvent(JSON.parse(msg.data))
          })
        } catch {
          // 网络错误或流中断,统一走下面的重连
        }
        setConnected(false)
        if (ctrl.signal.aborted) return
        await sleep(2000)
      }
    }
    run()

    return () => {
      ctrl.abort() // 组件卸载/切换 run:中断流与重连循环
      setConnected(false)
    }
  }, [runId])

  return { events, connected }
}

interface SSEMessage {
  id: number
  event: string
  data: string
}

// 极简 SSE 解析:按空行切消息块,只认后端实际输出的 id/event/data 三种字段,
// 注释行(如心跳 ": heartbeat")天然被忽略
async function readSSE(
  body: ReadableStream<Uint8Array>,
  onMessage: (msg: SSEMessage) => void,
) {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) return
    buf += decoder.decode(value, { stream: true })
    let idx: number
    while ((idx = buf.indexOf('\n\n')) !== -1) {
      const block = buf.slice(0, idx)
      buf = buf.slice(idx + 2)
      const msg: SSEMessage = { id: 0, event: '', data: '' }
      for (const line of block.split('\n')) {
        if (line.startsWith('id: ')) msg.id = Number(line.slice(4))
        else if (line.startsWith('event: ')) msg.event = line.slice(7)
        else if (line.startsWith('data: ')) msg.data = line.slice(6)
      }
      if (msg.data) onMessage(msg)
    }
  }
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
