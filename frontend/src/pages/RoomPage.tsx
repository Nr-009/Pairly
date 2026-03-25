import { useState, useEffect } from "react"
import { useParams, useNavigate } from "react-router-dom"
import { useAuth, apiFetch } from "../context/AuthContext"
import { Editor } from "../components/Editor/Editor"
import "./room.css"

interface File {
  id:       string
  name:     string
  language: string
}

export default function RoomPage() {
  const { roomId } = useParams<{ roomId: string }>()
  const { user }   = useAuth()
  const navigate   = useNavigate()

  const [role, setRole]         = useState<string | null>(null)
  const [files, setFiles]       = useState<File[]>([])
  const [activeFile, setActiveFile] = useState<File | null>(null)
  const [loading, setLoading]   = useState(true)
  const [error, setError]       = useState("")

  useEffect(() => {
    const load = async () => {
      try {
        // Check role and load files in parallel
        const [roleData, filesData] = await Promise.all([
          apiFetch(`/api/rooms/${roomId}/role`),
          apiFetch(`/api/rooms/${roomId}/files`),
        ])

        setRole(roleData.role)
        const fileList = filesData.files ?? []
        setFiles(fileList)

        // Load first file by default
        if (fileList.length > 0) {
          setActiveFile(fileList[0])
        }
      } catch {
        setError("You do not have access to this room.")
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [roomId])

  if (loading) {
    return (
      <div className="room-loading">
        <div className="room-spinner" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="room-error">
        <p>{error}</p>
        <button onClick={() => navigate("/dashboard")}>Back to dashboard</button>
      </div>
    )
  }

  return (
    <div className="room">
      <header className="room-header">
        <button className="room-back" onClick={() => navigate("/dashboard")}>
          ← Dashboard
        </button>
        <div className="room-info">
          <span className="room-active-file">{activeFile?.name ?? ""}</span>
          <span className="room-role" data-role={role}>{role}</span>
        </div>
        <span className="room-user">{user?.email}</span>
      </header>

      <div className="room-body">
        {/* File sidebar */}
        <aside className="room-sidebar">
          <div className="room-sidebar-title">Files</div>
          {files.map((file) => (
            <button
              key={file.id}
              className={`room-file-item ${activeFile?.id === file.id ? "active" : ""}`}
              onClick={() => setActiveFile(file)}
            >
              <svg width="13" height="13" viewBox="0 0 16 16" fill="none">
                <path d="M3 2h7l3 3v9H3V2z" stroke="currentColor" strokeWidth="1" strokeLinejoin="round"/>
                <path d="M10 2v3h3" stroke="currentColor" strokeWidth="1" strokeLinejoin="round"/>
              </svg>
              {file.name}
            </button>
          ))}
        </aside>

        {/* Editor area */}
        <div className="room-editor">
          {role === "viewer" && (
            <div className="room-viewer-banner">
              Read only — you can view but not edit
            </div>
          )}
          {activeFile && roomId && (
            <Editor
              roomId={roomId}
              fileId={activeFile.id}
              role={role as "owner" | "editor" | "viewer"}
              language={activeFile.language}
            />
          )}
        </div>
      </div>
    </div>
  )
}