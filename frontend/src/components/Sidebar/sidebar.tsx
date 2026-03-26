import { useState } from "react"
import { apiFetch } from "../../context/AuthContext"
import type { OnlineUser } from "../../hooks/useRoomSocket"
import { MembersList } from "./memberlist"
import "./sidebar.css"

interface File {
  id:       string
  name:     string
  language: string
}

interface Member {
  user_id: string
  name:    string
  role:    string
}

interface Props {
  roomId:          string
  files:           File[]
  activeFile:      File | null
  members:         Member[]
  currentRole:     string
  onlineUsers:     Map<string, OnlineUser>
  onFileClick:     (file: File) => void
  onMembersChange: (members: Member[]) => void
}

export function Sidebar({
  roomId,
  files,
  activeFile,
  members,
  currentRole,
  onlineUsers,
  onFileClick,
  onMembersChange,
}: Props) {
  const isOwner = currentRole === "owner"

  const [inviteEmail, setInviteEmail] = useState("")
  const [inviteRole, setInviteRole]   = useState<"editor" | "viewer">("editor")
  const [inviting, setInviting]       = useState(false)
  const [inviteError, setInviteError] = useState("")

  const handleInvite = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!inviteEmail.trim()) return
    setInviteError("")
    setInviting(true)
    try {
      const data = await apiFetch(`/api/rooms/${roomId}/members`, {
        method: "POST",
        body:   JSON.stringify({ email: inviteEmail.trim(), role: inviteRole }),
      })
      onMembersChange([...members, {
        user_id: data.user.id,
        name:    data.user.name,
        role:    inviteRole,
      }])
      setInviteEmail("")
    } catch (err: any) {
      setInviteError(err.message ?? "Could not invite")
    } finally {
      setInviting(false)
    }
  }

  return (
    <aside className="sidebar">

      <div className="sidebar-section">
        <div className="sidebar-label">Files</div>
        {files.map((file) => (
          <button
            key={file.id}
            className={`sidebar-file ${activeFile?.id === file.id ? "active" : ""}`}
            onClick={() => onFileClick(file)}
          >
            <svg width="13" height="13" viewBox="0 0 16 16" fill="none">
              <path d="M3 2h7l3 3v9H3V2z" stroke="currentColor" strokeWidth="1" strokeLinejoin="round"/>
              <path d="M10 2v3h3" stroke="currentColor" strokeWidth="1" strokeLinejoin="round"/>
            </svg>
            {file.name}
          </button>
        ))}
      </div>

      <div className="sidebar-divider" />

      <div className="sidebar-section">
        <div className="sidebar-label">Members</div>
        <MembersList
          roomId={roomId}
          members={members}
          currentRole={currentRole}
          onlineUsers={onlineUsers}
          onMembersChange={onMembersChange}
        />

        {isOwner && (
          <form className="sidebar-invite" onSubmit={handleInvite}>
            <div className="sidebar-label" style={{ marginTop: "0.75rem" }}>Invite</div>
            <input
              type="email"
              placeholder="email"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              className="sidebar-invite-input"
            />
            <div className="sidebar-invite-row">
              <select
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as "editor" | "viewer")}
                className="sidebar-invite-select"
              >
                <option value="editor">editor</option>
                <option value="viewer">viewer</option>
              </select>
              <button
                type="submit"
                className="sidebar-invite-btn"
                disabled={inviting}
              >
                {inviting ? "..." : "Add"}
              </button>
            </div>
            {inviteError && <p className="sidebar-invite-error">{inviteError}</p>}
          </form>
        )}
      </div>

    </aside>
  )
}