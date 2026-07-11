export type RunStatus =
  | 'pending' | 'running' | 'cancelling' | 'waiting_approval'
  | 'succeeded' | 'failed' | 'cancelled'

export interface Run {
  id: string
  goal: string
  status: RunStatus
  error: string | null
  created_at: string
  updated_at: string
}

// approval_requested 事件的 payload,镜像后端 engine.ApprovalRequestedPayload
export interface ApprovalRequest {
  tool_call_id: string
  tool_name: string
  arguments: string
  reason: string
}

export interface RunEvent {
  id: number
  run_id: string
  seq: number
  type: string
  payload: Record<string, unknown>
  created_at: string
}