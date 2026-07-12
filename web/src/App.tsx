import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Input, Layout, Typography } from 'antd'
import { LockOutlined } from '@ant-design/icons'
import { getToken, onUnauthorized, setToken } from './api/auth'
import { RunList } from './pages/RunList'
import { RunDetail } from './pages/RunDetail'

const { Header, Content } = Layout

export default function App() {
  const [selectedRun, setSelectedRun] = useState<string | null>(null)
  // 任一请求(含 SSE)撞到 401 就亮起;不预判后端是否开了鉴权(开发模式 token 为空)
  const [needAuth, setNeedAuth] = useState(false)

  useEffect(() => onUnauthorized(() => setNeedAuth(true)), [])

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header
        style={{
          position: 'sticky',
          top: 0,
          zIndex: 10,
          display: 'flex',
          alignItems: 'center',
          padding: '0 32px',
          borderBottom: '1px solid #ebebef',
          backdropFilter: 'blur(8px)',
        }}
      >
        <Typography.Text strong style={{ fontSize: 17, letterSpacing: '-0.02em' }}>
          Relay
        </Typography.Text>
        <Typography.Text type="secondary" style={{ marginLeft: 12, fontSize: 13 }}>
          Agent 任务编排
        </Typography.Text>
      </Header>

      <Content style={{ padding: '40px 32px 64px' }}>
        <div style={{ maxWidth: 880, margin: '0 auto' }}>
          {needAuth ? (
            // 替换整个内容区:页面组件卸载后轮询/SSE 都会停,不再刷 401
            <TokenGate onDone={() => setNeedAuth(false)} />
          ) : selectedRun ? (
            <RunDetail runId={selectedRun} onBack={() => setSelectedRun(null)} />
          ) : (
            <RunList onSelect={setSelectedRun} />
          )}
        </div>
      </Content>
    </Layout>
  )
}

function TokenGate({ onDone }: { onDone: () => void }) {
  const qc = useQueryClient()
  const [value, setValue] = useState('')
  // 已存过 token 还进到这里 → 说明旧 token 失效了,给出提示
  const [hadToken] = useState(() => getToken() !== '')

  const submit = () => {
    const token = value.trim()
    if (!token) return
    setToken(token)
    // 之前 401 的查询都失效重取;若新 token 仍无效,下一个 401 会再次打开本页
    qc.invalidateQueries()
    onDone()
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', paddingTop: 64 }}>
      <Card
        variant="borderless"
        style={{ width: 420, boxShadow: '0 1px 3px rgba(0,0,0,0.06)' }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div>
            <Typography.Title level={4} style={{ marginBottom: 4 }}>
              <LockOutlined style={{ marginRight: 8 }} />
              需要访问令牌
            </Typography.Title>
            <Typography.Text type="secondary" style={{ fontSize: 13 }}>
              服务端已开启鉴权,请输入 API_TOKEN 以继续
            </Typography.Text>
          </div>
          {hadToken && (
            <Alert type="warning" showIcon message="已保存的令牌无效或已过期,请重新输入" />
          )}
          <Input.Password
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onPressEnter={submit}
            placeholder="访问令牌"
            autoFocus
          />
          <Button type="primary" block disabled={!value.trim()} onClick={submit}>
            确认
          </Button>
        </div>
      </Card>
    </div>
  )
}
