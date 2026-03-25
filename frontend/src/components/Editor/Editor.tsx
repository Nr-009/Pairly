import { useState, useEffect } from "react"
import MonacoEditor from "@monaco-editor/react"
import type { editor as MonacoEditorType } from "monaco-editor"
import { useYjs } from "../../hooks/useYjs"
import { StatusBar } from "../StatusBar/StatusBar"
import { KeyMod, KeyCode } from "monaco-editor"

interface Props {
  roomId:   string
  fileId:   string
  role:     "owner" | "editor" | "viewer"
  language: string
}

export function Editor({ roomId, fileId, role, language }: Props) {
  const [editorInstance, setEditorInstance] =
    useState<MonacoEditorType.IStandaloneCodeEditor | null>(null)

  const { status, save } = useYjs({ roomId, fileId, monacoEditor: editorInstance })

useEffect(() => {
  if (!editorInstance) return
  const disposable = editorInstance.addAction({
    id:          "save-document",
    label:       "Save Document",
    keybindings: [KeyMod.CtrlCmd | KeyCode.KeyS],
    run:         () => save(),
  })
  return () => disposable.dispose()
}, [editorInstance, save])

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <MonacoEditor
        height="calc(100% - 32px)"
        language={language}
        theme="vs-dark"
        onMount={(editor) => setEditorInstance(editor)}
        options={{
          fontSize: 14,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          readOnly: role === "viewer",
        }}
      />
      <StatusBar status={status} />
    </div>
  )
}