import { useState } from 'react'
import { Layout, Typography } from 'antd'
import { RunList } from './pages/RunList'
import { RunDetail } from './pages/RunDetail'

const { Header, Content } = Layout

export default function App() {
  const [selectedRun, setSelectedRun] = useState<string | null>(null)

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
          {selectedRun ? (
            <RunDetail runId={selectedRun} onBack={() => setSelectedRun(null)} />
          ) : (
            <RunList onSelect={setSelectedRun} />
          )}
        </div>
      </Content>
    </Layout>
  )
}
