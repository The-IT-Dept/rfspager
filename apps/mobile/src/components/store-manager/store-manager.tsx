import { useEffect } from "react"
import notifee from "@notifee/react-native"
import { setUserEmail, setUserId } from "../../utils/analytics"
import { useAuthStore } from "../../stores"

type Props = {
  children: any
}

export function StoreManager(props: Props) {
  const user = useAuthStore((s) => s.user)
  const markNotificationsRead = useAuthStore((s) => s.markNotificationsRead)

  useEffect(() => {
    const clearBadge = async () => {
      const badgeCount = await notifee.getBadgeCount()
      if (badgeCount) {
        const ok = await markNotificationsRead()
        await notifee.setBadgeCount(ok ? 0 : badgeCount)
      }
    }

    if (user?.user_id) {
      setUserId(user.user_id)
      setUserEmail(user.email)
      clearBadge()
    }
  }, [user?.user_id])

  return props.children
}
