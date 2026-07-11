import { Tag } from 'antd'
import type { RunStatus } from '../api/types'

const STATUS_META: Record<RunStatus, { color: string; label: string }> = {
  pending: { color: 'default', label: '等待中' },
  running: { color: 'processing', label: '运行中' },
  cancelling: { color: 'warning', label: '取消中' },
  waiting_approval: { color: 'gold', label: '待审批' },
  succeeded: { color: 'success', label: '已完成' },
  failed: { color: 'error', label: '失败' },
  cancelled: { color: 'default', label: '已取消' },
}

export function StatusTag({ status }: { status: RunStatus }) {
  const meta = STATUS_META[status] ?? { color: 'default', label: status }
  return (
    <Tag color={meta.color} bordered={false} style={{ marginInlineEnd: 0 }}>
      {meta.label}
    </Tag>
  )
}
