import { useState, useEffect } from "react"
import { apiFetch } from "../../context/AuthContext"
import type { OnlineUser } from "../../hooks/useRoomSocket"
import { getUserColor } from "../../hooks/useRoomSocket"

const PRESENCE_TTL = 25_000

interface Member {
  user_id: string
  name:    string
  role:    string
}

interface Props {
  roomId:          string
  members:         Member[]
  currentRole:     string
  onlineUsers:     Map<string, OnlineUser>
  onMembersChange: (members: Member[]) => void
}

export function MembersList({
  roomId,
  members,
  currentRole,
  onlineUsers,
  onMembersChange,
}: Props) {
  const isOwner      = currentRole === "owner"
  const [, tick]     = useState(0)

  // Re-render every 75s so dots go gray when a user stops sending heartbeats.
  // No re-render happens in between — heartbeat arrivals trigger their own
  // re-render naturally via setOnlineUsers in useRoomSocket.
  useEffect(() => {
    const id = setInterval(() => tick(n => n + 1), 10_000)
    return () => clearInterval(id)
  }, [])

  const isOnline = (userId: string): boolean => {
    const user = onlineUsers.get(userId)
    return user !== undefined && Date.now() - user.lastSeen < PRESENCE_TTL
  }

  const handleRoleChange = async (userId: string, newRole: string) => {
    try {
      await apiFetch(`/api/rooms/${roomId}/members/${userId}`, {
        method: "PATCH",
        body:   JSON.stringify({ role: newRole }),
      })
      onMembersChange(
        members.map((m) => m.user_id === userId ? { ...m, role: newRole } : m)
      )
    } catch (err) {
      console.error("could not update role:", err)
    }
  }

  const handleRemove = async (userId: string) => {
    try {
      await apiFetch(`/api/rooms/${roomId}/members/${userId}`, {
        method: "DELETE",
      })
      onMembersChange(members.filter((m) => m.user_id !== userId))
    } catch (err) {
      console.error("could not remove member:", err)
    }
  }

  return (
    <>
      {members.map((member) => {
        const online = isOnline(member.user_id)
        const color  = getUserColor(member.user_id)
        return (
          <div key={member.user_id} className="sidebar-member">
            <div className="sidebar-member-info">
              <div className="sidebar-member-top">
                <span
                  className="sidebar-presence-dot"
                  style={{
                    backgroundColor: online ? color : "transparent",
                    borderColor:     online ? color : "var(--text-muted)",
                  }}
                  title={online ? "online" : "offline"}
                />
                <span className="sidebar-member-name">{member.name}</span>
              </div>
              {isOwner && member.role !== "owner" ? (
                <select
                  className="sidebar-role-select"
                  value={member.role}
                  onChange={(e) => handleRoleChange(member.user_id, e.target.value)}
                >
                  <option value="editor">editor</option>
                  <option value="viewer">viewer</option>
                </select>
              ) : (
                <span className={`sidebar-role sidebar-role--${member.role}`}>
                  {member.role}
                </span>
              )}
            </div>
            {isOwner && member.role !== "owner" && (
              <button
                className="sidebar-remove"
                onClick={() => handleRemove(member.user_id)}
                title="Remove member"
              >
                ✕
              </button>
            )}
          </div>
        )
      })}
    </>
  )
}