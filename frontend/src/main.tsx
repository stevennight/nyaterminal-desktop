import { Component } from 'react'
import ReactDOM from 'react-dom/client'
import { App } from './App'
import './styles.css'

class RootErrorBoundary extends Component<
  { children: React.ReactNode },
  { error?: Error }
> {
  state: { error?: Error } = {}

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error) {
    console.error('Root render failed', error)
  }

  render() {
    if (!this.state.error) return this.props.children
    return <div className="auth-screen"><div className="auth-card">
      <div className="auth-brand"><div className="brand-mark large">N</div></div>
      <h1>界面渲染失败</h1>
      <p>账号管理页触发了前端异常。</p>
      <div className="form-error">{this.state.error.message || String(this.state.error)}</div>
    </div></div>
  }
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <RootErrorBoundary>
    <App />
  </RootErrorBoundary>
)
