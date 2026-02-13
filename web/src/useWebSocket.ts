import { useEffect, useRef } from 'react'

export interface WSMessage {
  type: string
  payload: unknown
  timestamp: string
}

export function useWebSocket(onMessage: (msg: WSMessage) => void) {
  const onMessageRef = useRef(onMessage)

  useEffect(() => {
    onMessageRef.current = onMessage
  }, [onMessage])

  useEffect(() => {
    let wsRef: WebSocket | null = null
    let timer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const url = `${proto}//${window.location.host}/api/ws`
      const ws = new WebSocket(url)

      ws.onmessage = (event) => {
        try {
          const msg: WSMessage = JSON.parse(event.data)
          onMessageRef.current(msg)
        } catch {
          // Ignore malformed messages
        }
      }

      ws.onclose = () => {
        wsRef = null
        timer = setTimeout(connect, 3000)
      }

      ws.onerror = () => {
        ws.close()
      }

      wsRef = ws
    }

    connect()
    return () => {
      if (timer) clearTimeout(timer)
      wsRef?.close()
      wsRef = null
    }
  }, [])
}
