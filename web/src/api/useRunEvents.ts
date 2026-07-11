import { useEffect, useRef, useState } from 'react'
import type { RunEvent } from './types'

export function useRunEvents(runId: string, onFinished?: () => void) {
  const [events, setEvents] = useState<RunEvent[]>([])
  const [connected, setConnected] = useState(false)
  // onFinished 存进 ref:避免它进依赖数组导致重连(见下文解释)
  const onFinishedRef = useRef(onFinished)
  onFinishedRef.current = onFinished

  useEffect(() => {
    // runId 变化时重置并重建连接
    setEvents([])
    const es = new EventSource(`/api/runs/${runId}/events/stream`)

    es.addEventListener('run_event', (e) => {
      const ev: RunEvent = JSON.parse(e.data)
      setEvents((prev) => {
        // 去重:重连补发可能与已收到的重叠(镜像后端 lastSent 逻辑)
        if (prev.length > 0 && ev.id <= prev[prev.length - 1].id) return prev
        return [...prev, ev]
      })
      if (ev.type === 'run_finished') onFinishedRef.current?.()
    })

    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false) // EventSource 会自动重连,只更新指示灯

    return () => es.close() // 组件卸载/切换 run:关连接
  }, [runId])

  return { events, connected }
}