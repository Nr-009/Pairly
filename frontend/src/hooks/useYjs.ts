import { useEffect, useRef, useState } from 'react'
import * as Y from 'yjs'
import { WebsocketProvider } from 'y-websocket'
import { editor as MonacoEditor } from 'monaco-editor'
import { MonacoBinding } from 'y-monaco'

export function useYjs(editor: MonacoEditor.IStandaloneCodeEditor | null) {
  const [status, setStatus] = useState<'connected' | 'disconnected'>('disconnected')
  const providerRef = useRef<WebsocketProvider | null>(null)
  const ydocRef = useRef<Y.Doc | null>(null)

  useEffect(() => {
    if (!editor) return

    const wsUrl = import.meta.env.VITE_WS_URL ?? 'ws://localhost:8080/ws'

    const ydoc = new Y.Doc()
    const ytext = ydoc.getText('content')
    const provider = new WebsocketProvider(wsUrl, 'test', ydoc)

    ydocRef.current = ydoc
    providerRef.current = provider

    provider.on('status', (event: { status: string }) => {
      setStatus(event.status === 'connected' ? 'connected' : 'disconnected')
    })

    const model = editor.getModel()
    if (model) {
      new MonacoBinding(ytext, model, new Set([editor]), provider.awareness)
    }

    return () => {
      provider.destroy()
      ydoc.destroy()
    }
  }, [editor])

  return { status }
}