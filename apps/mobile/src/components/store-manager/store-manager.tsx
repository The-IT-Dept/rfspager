import { useEffect } from "react"
import notifee from "@notifee/react-native"
import { setUserEmail, setUserId } from "../../utils/analytics"
import { useAuthStore, useSubscriptionsStore } from "../../stores"

type Props = {
  children: any
}

export function StoreManager(props: Props) {
  const user = useAuthStore((s) => s.user)
  const markNotificationsRead = useAuthStore((s) => s.markNotificationsRead)
  const loadSubscriptions = useSubscriptionsStore((s) => s.load)
  const resetSubscriptions = useSubscriptionsStore((s) => s.reset)

  // The pager needs the saved filter list before it can tell "nothing
  // selected" from "not fetched yet"; the capcodes screen used to be the only
  // place that fetched it.
  useEffect(() => {
    if (user?.user_id) {
      loadSubscriptions()
    } else {
      resetSubscriptions()
    }
  }, [user?.user_id])

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
