import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Empty, Input, Table, Typography } from 'antd'
import { ArrowUpOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../api/client'
import { StatusTag } from '../components/StatusTag'
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

  const submit = () => {
    if (goal.trim() && !createMutation.isPending) createMutation.mutate(goal.trim())
  }

  const columns: ColumnsType<Run> = [
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: RunStatus) => <StatusTag status={status} />,
    },
    {
      title: '目标',
      dataIndex: 'goal',
      ellipsis: true,
      render: (goal: string) => <Typography.Text>{goal}</Typography.Text>,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 180,
      render: (t: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          {new Date(t).toLocaleString()}
        </Typography.Text>
      ),
    },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 32 }}>
      <div>
        <Typography.Title level={3} style={{ marginBottom: 4, letterSpacing: '-0.02em' }}>
          任务
        </Typography.Title>
        <Typography.Text type="secondary">
          描述一个目标,Relay 会自主规划并执行
        </Typography.Text>
      </div>

      <Card variant="borderless" styles={{ body: { padding: 8 } }} style={{ boxShadow: '0 1px 3px rgba(0,0,0,0.06)' }}>
        <div style={{ display: 'flex', gap: 8 }}>
          <Input.TextArea
            value={goal}
            onChange={(e) => setGoal(e.target.value)}
            onPressEnter={(e) => {
              if (!e.shiftKey) {
                e.preventDefault()
                submit()
              }
            }}
            placeholder="描述你要 Relay 完成的任务…"
            autoSize={{ minRows: 1, maxRows: 5 }}
            variant="borderless"
            style={{ fontSize: 15, padding: '8px 12px' }}
          />
          <Button
            type="primary"
            shape="circle"
            icon={<ArrowUpOutlined />}
            onClick={submit}
            loading={createMutation.isPending}
            disabled={!goal.trim()}
            style={{ alignSelf: 'flex-end', marginBottom: 4, marginRight: 4 }}
          />
        </div>
      </Card>

      {createMutation.isError && (
        <Alert type="error" showIcon message={createMutation.error.message} />
      )}
      {runsQuery.isError && (
        <Alert type="error" showIcon message={`加载失败:${runsQuery.error.message}`} />
      )}

      <Table
        rowKey="id"
        columns={columns}
        dataSource={runsQuery.data}
        loading={runsQuery.isPending}
        pagination={false}
        onRow={(run) => ({
          onClick: () => onSelect(run.id),
          style: { cursor: 'pointer' },
        })}
        locale={{
          emptyText: (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有任务,从上方创建一个" />
          ),
        }}
        style={{
          background: '#fff',
          borderRadius: 12,
          boxShadow: '0 1px 3px rgba(0,0,0,0.06)',
          overflow: 'hidden',
        }}
      />
    </div>
  )
}
