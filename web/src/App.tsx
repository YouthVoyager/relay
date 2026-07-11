import { useState } from 'react'
import { RunList } from './pages/RunList'
import { RunDetail } from './pages/RunDetail'

export default function App() {
  const [selectedRun, setSelectedRun] = useState<string | null>(null)

  return (
    <div className="app">
      <h1>Relay</h1>
      {selectedRun ? (
        <div>
          <button onClick={() => setSelectedRun(null)}>← 返回列表</button>
          <RunDetail runId={selectedRun} />
        </div>
      ) : (
        <RunList onSelect={setSelectedRun} />
      )}
    </div>
  )
}