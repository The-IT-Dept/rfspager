import * as React from "react"
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native"
import { SafeAreaView } from "react-native-safe-area-context"
import { Ionicons } from "@expo/vector-icons"
import { useRouter, usePathname } from "expo-router"
import { AppTheme, useAppTheme } from "../../theme"
import { useUser } from "../../stores"

type IconName = React.ComponentProps<typeof Ionicons>["name"]

type NavItem = {
  label: string
  icon: IconName
  path: string
  match: (pathname: string) => boolean
}

const NAV_ITEMS: NavItem[] = [
  {
    label: "Pager",
    icon: "radio",
    path: "/(tabs)/(stack)/",
    match: (p) => p === "/" || p === "",
  },
  {
    label: "Incidents",
    icon: "flame",
    path: "/(tabs)/(stack)/incidents",
    match: (p) => p.startsWith("/incidents"),
  },
  {
    label: "Map",
    icon: "map",
    path: "/(tabs)/(stack)/map",
    match: (p) => p.startsWith("/map"),
  },
  {
    label: "Systems",
    icon: "hardware-chip",
    path: "/(tabs)/(stack)/systems",
    match: (p) => p.startsWith("/systems"),
  },
  {
    label: "Settings",
    icon: "settings",
    path: "/(tabs)/(stack)/settings",
    match: (p) => p.startsWith("/settings"),
  },
]

export function DrawerContent(props: any) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const pathname = usePathname()
  const user = useUser()

  const go = (path: string) => {
    props.navigation?.closeDrawer?.()
    router.replace(path as any)
  }

  return (
    <SafeAreaView style={styles.root} edges={["top", "bottom"]}>
      <View style={styles.header}>
        <Text style={styles.appName}>RFSPager</Text>
        {user?.name ? <Text style={styles.userName}>{user.name}</Text> : null}
        {user?.email ? <Text style={styles.userEmail}>{user.email}</Text> : null}
      </View>

      <ScrollView contentContainerStyle={styles.scroll}>
        {NAV_ITEMS.map((item) => {
          const focused = item.match(pathname)
          return (
            <Pressable
              key={item.path}
              onPress={() => go(item.path)}
              style={({ pressed }) => [
                styles.item,
                focused && styles.itemFocused,
                pressed && !focused && styles.itemPressed,
              ]}
            >
              <Ionicons
                name={item.icon}
                size={20}
                color={focused ? theme.colors.primary : theme.colors.textSecondary}
              />
              <Text style={[styles.itemLabel, focused && styles.itemLabelFocused]}>
                {item.label}
              </Text>
            </Pressable>
          )
        })}
      </ScrollView>
    </SafeAreaView>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    root: {
      flex: 1,
      backgroundColor: theme.colors.cardBackground,
    },
    header: {
      paddingHorizontal: 20,
      paddingTop: 16,
      paddingBottom: 16,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    appName: {
      fontSize: 22,
      fontWeight: "700",
      color: theme.colors.primary,
      marginBottom: 6,
    },
    userName: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    userEmail: {
      fontSize: 13,
      color: theme.colors.textSecondary,
      marginTop: 2,
    },
    scroll: {
      paddingTop: 8,
      paddingHorizontal: 8,
    },
    item: {
      flexDirection: "row",
      alignItems: "center",
      gap: 14,
      paddingHorizontal: 12,
      paddingVertical: 11,
      borderRadius: 10,
      marginBottom: 2,
    },
    itemFocused: {
      backgroundColor: theme.colors.primary + "15",
    },
    itemPressed: {
      backgroundColor: theme.colors.backgroundTertiary,
    },
    itemLabel: {
      fontSize: 16,
      fontWeight: "500",
      color: theme.colors.textPrimary,
    },
    itemLabelFocused: {
      color: theme.colors.primary,
      fontWeight: "600",
    },
  })
