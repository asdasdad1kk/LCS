import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { Toaster } from 'sonner'
import App from './App'
import { ThemeProvider } from '@/components/theme-provider'
import './index.css'

declare global {
  interface Window {
    go?: { main?: { App?: {
      Minimise?: () => void
      ToggleMaximise?: () => void
      Quit?: () => void
    } } }
    runtime?: { WindowIsMaximised?: () => Promise<boolean> }
  }
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider>
      <BrowserRouter>
        <App />
        <Toaster position="bottom-right" richColors closeButton toastOptions={{ duration: 4000 }} />
      </BrowserRouter>
    </ThemeProvider>
  </React.StrictMode>
)
