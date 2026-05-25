import * as React from "react"
import { Pressable } from "react-native"
import { Ionicons } from "@expo/vector-icons"
import { useNavigation } from "expo-router"
import { useAppTheme } from "../../theme"

type DrawerNav = { toggleDrawer?: () => void }

export function DrawerToggle() {
  const theme = useAppTheme()
  const nav = useNavigation() as unknown as DrawerNav

  return (
    <Pressable
      accessibilityLabel="Open navigation menu"
      hitSlop={12}
      onPress={() => nav.toggleDrawer?.()}
      style={{ paddingHorizontal: 4 }}
    >
      <Ionicons name="menu" size={22} color={theme.colors.textPrimary} />
    </Pressable>
  )
}
