import { useLocalSearchParams } from "expo-router"
import { MySystemScreen } from "../../../../screens/systems/my-system-screen"

export default function MySystemDetail() {
  const { uuid } = useLocalSearchParams<{ uuid: string }>()
  return <MySystemScreen uuid={uuid} />
}
