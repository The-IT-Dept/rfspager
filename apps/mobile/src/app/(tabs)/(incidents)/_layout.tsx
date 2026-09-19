import { Stack } from "expo-router"
import { useAppTheme } from "../../../theme"
import { stackScreenOptions } from "../../../navigation/stack-screen-options"

export default function IncidentsStackLayout() {
  const theme = useAppTheme()
  return (
    <Stack screenOptions={stackScreenOptions(theme)}>
      <Stack.Screen name="incidents" options={{ title: "Incidents" }} />
      <Stack.Screen name="incident/[id]" options={{ title: "Incident" }} />
    </Stack>
  )
}
