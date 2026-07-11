import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Alert, Badge, Button, Card, Collapse, Space, Spin, Tag, Timeline, Typography } from 'antd'
import {
  ArrowLeftOutlined,
  CheckCircleFilled,
  CloseCircleFilled,
  CompressOutlined,
  LoadingOutlined,
  PlayCircleOutlined,
  SafetyCertificateOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import { api } from '../api/client'
import { useRunEvents } from '../api/useRunEvents'
import { StatusTag } from '../components/StatusTag'
import type { ApprovalRequest, RunEvent } from '../api/types'

export function RunDetail({ runId, onBack }: { runId: string; onBack: () => void }) {
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

  const approvalMutation = useMutation({
    mutationFn: ({ toolCallId, approved }: { toolCallId: string; approved: boolean }) =>
      api.decideApproval(runId, toolCallId, approved),
    onSuccess: () => {
      // 决定后 run 恢复运行,状态变了;后续事件由 SSE 推进来
      qc.invalidateQueries({ queryKey: ['runs', runId] })
      qc.invalidateQueries({ queryKey: ['runs'] })
    },
  })

  const run = runQuery.data
  if (!run) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', padding: 80 }}>
        <Spin />
      </div>
    )
  }

  const active = ['pending', 'running', 'waiting_approval'].includes(run.status)

  // 从事件流重建待审批项(镜像后端 loadApprovalState):按序扫描,requested
  // 挂起、decided 消除。同一 tool_call_id 可能有多轮审批(崩溃后的重跑确认
  // 复用原 ID),所以不能用"decided 过的 ID 集合"一刀切过滤。
  const pendingMap = new Map<string, ApprovalRequest>()
  for (const ev of events) {
    const id = (ev.payload as Record<string, unknown>).tool_call_id as string
    if (ev.type === 'approval_requested') {
      pendingMap.set(id, ev.payload as unknown as ApprovalRequest)
    } else if (ev.type === 'approval_decided') {
      pendingMap.delete(id)
    }
  }
  const pendingApproval =
    run.status === 'waiting_approval' ? pendingMap.values().next().value : undefined

  const items = events.map((ev) => eventItem(ev))
  if (active && !pendingApproval) {
    items.push({
      key: 'pending',
      dot: <LoadingOutlined style={{ color: '#71717a' }} />,
      children: (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          {run.status === 'waiting_approval' ? '等待审批…' : '执行中…'}
        </Typography.Text>
      ),
    })
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <div>
        <Button type="text" icon={<ArrowLeftOutlined />} onClick={onBack} style={{ marginLeft: -12, color: '#71717a' }}>
          返回列表
        </Button>
      </div>

      <Card variant="borderless" style={{ boxShadow: '0 1px 3px rgba(0,0,0,0.06)' }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16 }}>
          <div>
            <Space size={8} style={{ marginBottom: 8 }}>
              <StatusTag status={run.status} />
              <Badge
                status={connected ? 'processing' : 'default'}
                text={
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {connected ? '实时' : '离线'}
                  </Typography.Text>
                }
              />
            </Space>
            <Typography.Paragraph style={{ fontSize: 16, fontWeight: 500, marginBottom: 0, letterSpacing: '-0.01em' }}>
              {run.goal}
            </Typography.Paragraph>
          </div>
          {active && (
            <Button danger onClick={() => cancelMutation.mutate()} loading={cancelMutation.isPending}>
              取消任务
            </Button>
          )}
        </div>
        {run.error && <Alert type="error" showIcon message={run.error} style={{ marginTop: 16 }} />}
      </Card>

      {pendingApproval && (
        <Card
          variant="borderless"
          style={{
            boxShadow: '0 1px 3px rgba(0,0,0,0.06)',
            borderLeft: '3px solid #faad14',
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Space size={8}>
              <SafetyCertificateOutlined style={{ color: '#faad14', fontSize: 16 }} />
              <Typography.Text strong>高危操作待审批</Typography.Text>
              <Typography.Text code style={{ fontSize: 12 }}>
                {pendingApproval.tool_name}
              </Typography.Text>
            </Space>
            {pendingApproval.reason && (
              <Typography.Text style={{ fontSize: 13 }}>{pendingApproval.reason}</Typography.Text>
            )}
            <pre style={{ margin: 0 }}>{formatArgs(pendingApproval.arguments)}</pre>
            {approvalMutation.isError && (
              <Alert type="error" showIcon message={approvalMutation.error.message} />
            )}
            <Space size={8}>
              <Button
                type="primary"
                loading={approvalMutation.isPending && approvalMutation.variables?.approved === true}
                disabled={approvalMutation.isPending}
                onClick={() =>
                  approvalMutation.mutate({ toolCallId: pendingApproval.tool_call_id, approved: true })
                }
              >
                批准执行
              </Button>
              <Button
                danger
                loading={approvalMutation.isPending && approvalMutation.variables?.approved === false}
                disabled={approvalMutation.isPending}
                onClick={() =>
                  approvalMutation.mutate({ toolCallId: pendingApproval.tool_call_id, approved: false })
                }
              >
                拒绝
              </Button>
            </Space>
          </div>
        </Card>
      )}

      <Card variant="borderless" style={{ boxShadow: '0 1px 3px rgba(0,0,0,0.06)' }}>
        <Timeline items={items} style={{ paddingTop: 8 }} />
      </Card>
    </div>
  )
}

// 参数原文是模型生成的 JSON 字符串,能解析就美化,不能就原样展示
function formatArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2)
  } catch {
    return args
  }
}

function eventItem(event: RunEvent) {
  const p = event.payload as Record<string, any>
  const key = String(event.id)

  switch (event.type) {
    case 'run_started':
      return {
        key,
        dot: <PlayCircleOutlined style={{ color: '#71717a' }} />,
        children: (
          <Typography.Text type="secondary" style={{ fontSize: 13 }}>
            任务开始
          </Typography.Text>
        ),
      }

    case 'llm_called':
      return {
        key,
        color: 'gray' as const,
        children: (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {p.content && (
              <Typography.Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>
                {p.content}
              </Typography.Paragraph>
            )}
            {p.tool_calls?.map((tc: any) => (
              <Typography.Text key={tc.id} code style={{ fontSize: 12, width: 'fit-content' }}>
                → {tc.name}({tc.arguments})
              </Typography.Text>
            ))}
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {p.prompt_tokens} tokens
            </Typography.Text>
          </div>
        ),
      }

    case 'tool_started':
      return {
        key,
        dot: <ToolOutlined style={{ color: '#71717a' }} />,
        children: (
          <Typography.Text type="secondary" style={{ fontSize: 13 }}>
            开始执行{' '}
            <Typography.Text code style={{ fontSize: 12 }}>
              {p.name}
            </Typography.Text>
          </Typography.Text>
        ),
      }

    case 'tool_executed':
      return {
        key,
        dot: <ToolOutlined style={{ color: '#71717a' }} />,
        children: (
          <Collapse
            ghost
            size="small"
            style={{ marginLeft: -16, marginTop: -4 }}
            items={[
              {
                key: 'r',
                label: (
                  <Typography.Text style={{ fontSize: 13 }}>
                    {p.name} <Typography.Text type="secondary">完成</Typography.Text>
                  </Typography.Text>
                ),
                children: <pre>{p.result}</pre>,
              },
            ]}
          />
        ),
      }

    case 'compaction':
      return {
        key,
        dot: <CompressOutlined style={{ color: '#71717a' }} />,
        children: (
          <Collapse
            ghost
            size="small"
            style={{ marginLeft: -16, marginTop: -4 }}
            items={[
              {
                key: 's',
                label: (
                  <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                    上下文已压缩(覆盖至 seq {p.through_seq})
                  </Typography.Text>
                ),
                children: <pre>{p.summary}</pre>,
              },
            ]}
          />
        ),
      }

    case 'approval_requested':
      return {
        key,
        dot: <SafetyCertificateOutlined style={{ color: '#faad14' }} />,
        children: (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Typography.Text style={{ fontSize: 13 }}>
              请求执行高危工具{' '}
              <Typography.Text code style={{ fontSize: 12 }}>
                {p.tool_name}
              </Typography.Text>
            </Typography.Text>
            {p.reason && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {p.reason}
              </Typography.Text>
            )}
          </div>
        ),
      }

    case 'approval_decided':
      return {
        key,
        dot: p.approved ? (
          <CheckCircleFilled style={{ color: '#52c41a' }} />
        ) : (
          <CloseCircleFilled style={{ color: '#ff4d4f' }} />
        ),
        children: (
          <Typography.Text style={{ fontSize: 13, color: p.approved ? '#52c41a' : '#ff4d4f' }}>
            {p.approved ? '审批通过,继续执行' : '已拒绝该操作'}
          </Typography.Text>
        ),
      }

    case 'run_finished': {
      const ok = p.status === 'succeeded'
      return {
        key,
        dot: ok ? (
          <CheckCircleFilled style={{ color: '#52c41a' }} />
        ) : (
          <CloseCircleFilled style={{ color: '#ff4d4f' }} />
        ),
        children: (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <Typography.Text strong style={{ color: ok ? '#52c41a' : '#ff4d4f' }}>
              {ok ? '完成' : p.status}
            </Typography.Text>
            {p.error && <pre>{p.error}</pre>}
          </div>
        ),
      }
    }

    default:
      // 前向兼容:后端新增事件类型时,老前端降级显示而不是崩溃
      return {
        key,
        color: 'gray' as const,
        children: <Tag bordered={false}>{event.type}</Tag>,
      }
  }
}
