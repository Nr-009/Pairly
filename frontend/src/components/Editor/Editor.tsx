import { useRef, useState } from 'react'
import MonacoEditor from '@monaco-editor/react'
import { editor as MonacoEditorType } from 'monaco-editor'
import { useYjs } from '../../hooks/useYjs'
import { StatusBar } from '../StatusBar/StatusBar'

export function Editor() {
  const [editorInstance, setEditorInstance] = useState<MonacoEditorType.IStandaloneCodeEditor | null>(null)
  const { status } = useYjs(editorInstance)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
      <MonacoEditor
        height="calc(100vh - 32px)"
        defaultLanguage="javascript"
        theme="vs-dark"
        onMount={(editor) => setEditorInstance(editor)}
        options={{
          fontSize: 14,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
        }}
      />
      <StatusBar status={status} />
    </div>
  )
}