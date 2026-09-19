import { Stack } from "expo-router"
import { useAppTheme } from "../../../theme"
import { stackScreenOptions } from "../../../navigation/stack-screen-options"

export default function MapStackLayout() {
  const theme = useAppTheme()
  return (
    <Stack screenOptions={stackScreenOptions(theme)}>
      {/* Title blank: the map is edge-to-edge and a title would sit over the canvas. */}
      <Stack.Screen name="map" options={{ title: "" }} />
    </Stack>
  )
}
