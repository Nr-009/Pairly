interface Props {
  status: 'connected' | 'disconnected'
}

export function StatusBar({ status }: Props) {
  return (
    <div style={{
      height: '32px',
      backgroundColor: '#1e1e1e',
      borderTop: '1px solid #333',
      display: 'flex',
      alignItems: 'center',
      paddingLeft: '16px',
      gap: '8px',
    }}>
      <div style={{
        width: '8px',
        height: '8px',
        borderRadius: '50%',
        backgroundColor: status === 'connected' ? '#4caf50' : '#f44336',
      }} />
      <span style={{ color: '#ccc', fontSize: '12px' }}>
        {status === 'connected' ? 'Connected' : 'Disconnected'}
      </span>
    </div>
  )
}