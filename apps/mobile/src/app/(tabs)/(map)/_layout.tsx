import { ExperimentalStack } from "expo-router"

// See (pager)/_layout.tsx for why the experimental stack is used.
export default function MapStackLayout() {
  return (
    <ExperimentalStack>
      {/* Title blank: the map is edge-to-edge and a title would sit over the canvas. */}
      <ExperimentalStack.Screen name="map" options={{ title: "" }} />
    </ExperimentalStack>
  )
}
