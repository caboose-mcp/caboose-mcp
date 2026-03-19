import { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Navbar from './components/Navbar'
import Footer from './components/Footer'
import ExperimentalBanner from './components/ExperimentalBanner'
import Home from './pages/Home'
import Tools from './pages/Tools'
import Sandbox from './pages/Sandbox'
import AuthPortal from './pages/AuthPortal'
import Changelog from './pages/Changelog'
import ToolDetail from './pages/ToolDetail'

export default function App() {
  const [experimental, setExperimental] = useState(true)

  useEffect(() => {
    fetch('/api/config')
      .then(r => r.json())
      .then((d: { env: string }) => setExperimental(d.env !== 'stable'))
      .catch(() => { /* default stays experimental */ })
  }, [])

  return (
    <BrowserRouter basename="/ui">
      <div className="min-h-screen flex flex-col">
        {experimental && <ExperimentalBanner />}
        <Navbar />
        <main className="flex-1">
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/tools" element={<Tools />} />
            <Route path="/tools/:name" element={<ToolDetail />} />
            <Route path="/sandbox" element={<Sandbox />} />
            <Route path="/auth" element={<AuthPortal />} />
            <Route path="/changelog" element={<Changelog />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
        <Footer />
      </div>
    </BrowserRouter>
  )
}
