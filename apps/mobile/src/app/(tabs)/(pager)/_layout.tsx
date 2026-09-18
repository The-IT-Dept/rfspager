import { Stack } from "expo-router"
import { useAppTheme } from "../../../theme"
import { stackScreenOptions } from "../../../navigation/stack-screen-options"

export default function PagerStackLayout() {
  const theme = useAppTheme()
  return (
    <Stack screenOptions={stackScreenOptions(theme)}>
      <Stack.Screen name="index" options={{ title: "Pager" }} />
      <Stack.Screen name="message/[id]" options={{ title: "Message" }} />
    </Stack>
  )
}
