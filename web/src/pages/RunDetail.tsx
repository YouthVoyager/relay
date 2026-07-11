import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useRunEvents } from '../api/useRunEvents'
import type { RunEvent } from '../api/types'

export function RunDetail({ runId }: { runId: string }) {
  const qc = useQueryClient()

  const runQuery = useQuery({
    queryKey: ['runs', runId],
    queryFn: () => api.getRun(runId),
  })

  const { events, connected } = useRunEvents(runId, () => {
    // run_finished 事件到达 → run 状态一定变了 → 精确失效
    qc.invalidateQueries({ queryKey: ['runs', runId] })
    qc.invalidateQueries({ queryKey: ['runs'] })
  })

  const cancelMutation = useMutation({
    mutationFn: () => api.cancelRun(runId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['runs', runId] }),
  })

  const run = runQuery.data
  if (!run) return <p>加载中…</p>

  const active = ['pending', 'running', 'waiting_approval'].includes(run.status)

  return (
    <div className="run-detail">
      <header>
        <span className={`badge badge-${run.status}`}>{run.status}</span>
        <span className={connected ? 'dot dot-on' : 'dot dot-off'} title="实时连接" />
        {active && (
          <button onClick={() => cancelMutation.mutate()} disabled={cancelMutation.isPending}>
            取消任务
          </button>
        )}
      </header>
      <p className="goal">{run.goal}</p>
      {run.error && <p className="error">{run.error}</p>}

      <div className="timeline">
        {events.map((ev) => <EventCard key={ev.id} event={ev} />)}
        {active && <p className="pending-indicator">执行中…</p>}
      </div>
    </div>
  )
}

function EventCard({ event }: { event: RunEvent }) {
  const p = event.payload as Record<string, any>
  switch (event.type) {
    case 'run_started':
      return <div className="ev ev-meta">▶ 任务开始</div>

    case 'llm_called':
      return (
        <div className="ev ev-llm">
          {p.content && <p>{p.content}</p>}
          {p.tool_calls?.map((tc: any) => (
            <code key={tc.id} className="tool-intent">
              → {tc.name}({tc.arguments})
            </code>
          ))}
          <span className="tokens">{p.prompt_tokens} tok</span>
        </div>
      )

    case 'tool_executed':
      return (
        <details className="ev ev-tool">
          <summary>🔧 {p.name} 完成</summary>
          <pre>{p.result}</pre>
        </details>
      )

    case 'compaction':
      return (
        <details className="ev ev-compact">
          <summary>📦 上下文已压缩(覆盖至 seq {p.through_seq})</summary>
          <pre>{p.summary}</pre>
        </details>
      )

    case 'run_finished':
      return (
        <div className={`ev ev-final ev-${p.status}`}>
          {p.status === 'succeeded' ? '✓ 完成' : `✗ ${p.status}`}
          {p.error && <pre>{p.error}</pre>}
        </div>
      )

    default:
      // 前向兼容:后端新增事件类型时,老前端降级显示而不是崩溃
      return <div className="ev">{event.type}</div>
  }
}