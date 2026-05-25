import React from "react"
import { ScrollView, View, StyleSheet, TouchableOpacity, Alert } from "react-native"
import { Text } from "react-native"
import { Gravatar } from "react-native-gravatar"
import { useUser, useAuthStore, useValidSubscription, useProductName } from "../../../../stores"
import { useAppTheme, AppTheme } from "../../../../theme"

export default function AccountScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const user = useUser()
  const logout = useAuthStore((s) => s.logout)
  const validSubscription = useValidSubscription()
  const productName = useProductName()

  const handleLogout = () => {
    Alert.alert("Log Out", "Are you sure you want to log out?", [
      { text: "Cancel", style: "cancel" },
      { text: "Log Out", style: "destructive", onPress: () => logout() },
    ])
  }

  return (
    <ScrollView
      style={styles.container}
      contentInsetAdjustmentBehavior="automatic"
      showsVerticalScrollIndicator={false}
    >
      <View style={styles.avatarSection}>
        <Gravatar
          style={styles.avatar}
          options={{
            email: user?.email || "",
            parameters: { size: "200", d: "robohash" },
            secure: true,
          }}
        />
      </View>

      <View style={styles.detailsCard}>
        <View style={styles.row}>
          <Text style={styles.label}>Name</Text>
          <Text style={styles.value}>{user?.name || "—"}</Text>
        </View>
        <View style={styles.row}>
          <Text style={styles.label}>Email</Text>
          <Text style={styles.value}>{user?.email || "—"}</Text>
        </View>
        <View style={[styles.row, styles.lastRow]}>
          <Text style={styles.label}>Subscription</Text>
          <Text style={[styles.value, validSubscription && { color: theme.colors.success }]}>
            {productName}
          </Text>
        </View>
      </View>

      <TouchableOpacity style={styles.logoutButton} onPress={handleLogout}>
        <Text style={styles.logoutText}>Log Out</Text>
      </TouchableOpacity>
    </ScrollView>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    avatarSection: {
      alignItems: "center",
      paddingVertical: 24,
    },
    avatar: {
      width: 80,
      height: 80,
      borderRadius: 40,
    },
    detailsCard: {
      marginHorizontal: 16,
      backgroundColor: theme.colors.cardBackground,
      overflow: "hidden",
    },
    row: {
      flexDirection: "row",
      justifyContent: "space-between",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 11,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    lastRow: {
      borderBottomWidth: 0,
    },
    label: {
      fontSize: 17,
      color: theme.colors.textPrimary,
    },
    value: {
      fontSize: 17,
      color: theme.colors.textSecondary,
    },
    logoutButton: {
      marginHorizontal: 16,
      marginTop: 16,
      backgroundColor: theme.colors.cardBackground,
      paddingVertical: 11,
      alignItems: "center",
    },
    logoutText: {
      fontSize: 17,
      color: theme.colors.error,
    },
  })
