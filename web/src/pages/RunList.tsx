import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Run, RunStatus } from '../api/types'

const ACTIVE: RunStatus[] = ['pending', 'running', 'cancelling', 'waiting_approval']

export function RunList({ onSelect }: { onSelect: (id: string) => void }) {
  const qc = useQueryClient()
  const [goal, setGoal] = useState('')

  const runsQuery = useQuery({
    queryKey: ['runs'],
    queryFn: api.listRuns,
    // 有活跃 run 时每 3 秒轮询,否则不轮询——按需消耗
    refetchInterval: (query) =>
      query.state.data?.some((r) => ACTIVE.includes(r.status)) ? 3000 : false,
  })

  const createMutation = useMutation({
    mutationFn: api.createRun,
    onSuccess: () => {
      setGoal('')
      qc.invalidateQueries({ queryKey: ['runs'] }) // 声明"列表脏了",Query 自动重取
    },
  })

  if (runsQuery.isPending) return <p>加载中…</p>
  if (runsQuery.isError) return <p className="error">加载失败: {runsQuery.error.message}</p>

  return (
    <div className="run-list">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (goal.trim()) createMutation.mutate(goal.trim())
        }}
      >
        <input
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          placeholder="描述你要 Relay 完成的任务…"
        />
        <button disabled={createMutation.isPending}>
          {createMutation.isPending ? '创建中…' : '创建任务'}
        </button>
        {createMutation.isError && <p className="error">{createMutation.error.message}</p>}
      </form>

      <table>
        <thead>
          <tr><th>状态</th><th>目标</th><th>创建时间</th></tr>
        </thead>
        <tbody>
          {runsQuery.data.map((run) => (
            <tr key={run.id} onClick={() => onSelect(run.id)}>
              <td><StatusBadge status={run.status} /></td>
              <td className="goal">{run.goal}</td>
              <td>{new Date(run.created_at).toLocaleString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function StatusBadge({ status }: { status: RunStatus }) {
  return <span className={`badge badge-${status}`}>{status}</span>
}