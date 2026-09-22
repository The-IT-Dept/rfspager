export interface SessionSnapshot {
  token: string
  userID: string
}

export function captureSession(token: string | null, userID: string | undefined): SessionSnapshot | null {
  return token && userID ? { token, userID } : null
}

export function sessionIsCurrent(
  snapshot: SessionSnapshot,
  current: { token: string | null; userID: string | undefined },
): boolean {
  return current.token === snapshot.token && current.userID === snapshot.userID
}
