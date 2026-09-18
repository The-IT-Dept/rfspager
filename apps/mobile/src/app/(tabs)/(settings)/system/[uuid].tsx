import { useLocalSearchParams } from "expo-router"

import { SystemDetailScreen } from "../../../../screens/systems/system-detail-screen"

export default function SystemDetail() {
  const { uuid } = useLocalSearchParams<{ uuid: string }>()
  if (!uuid) return null
  return <SystemDetailScreen uuid={uuid} />
}
