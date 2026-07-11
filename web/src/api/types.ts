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

export interface RunEvent {
  id: number
  run_id: string
  seq: number
  type: string
  payload: Record<string, unknown>
  created_at: string
}