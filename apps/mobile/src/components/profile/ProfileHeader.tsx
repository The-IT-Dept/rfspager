import React from "react"
import { View, StyleSheet, TouchableOpacity, Image } from "react-native"
import { Text } from "react-native"
import { Gravatar } from "react-native-gravatar"
import { Images } from "../../constants"
import { useAppTheme, AppTheme } from "../../theme"

interface ProfileHeaderProps {
  user: {
    name: string
    email: string
  }
  onLogout: () => void
  isLoading?: boolean
}

export const ProfileHeader: React.FC<ProfileHeaderProps> = (
  { user, onLogout, isLoading = false }) => {
    const theme = useAppTheme()
    const styles = makeStyles(theme)

    if (isLoading) {
      return (
        <View style={styles.container}>
          <View style={styles.loadingAvatar} />
          <View style={styles.userInfo}>
            <View style={styles.loadingName} />
            <View style={styles.loadingEmail} />
          </View>
        </View>
      )
    }

    return (
      <View style={styles.container}>
        <Gravatar
          style={styles.avatar}
          options={{
            email: user.email,
            parameters: { size: "200", d: "robohash" },
            secure: true,
          }}
        />
        <View style={styles.userInfo}>
          <Text style={styles.name}>{user.name}</Text>
          <Text style={styles.email}>{user.email}</Text>
        </View>
        <TouchableOpacity onPress={onLogout} style={styles.logoutButton}>
          <Image
            source={Images.signOut}
            style={[styles.logoutIcon, { tintColor: theme.colors.textSecondary }]}
          />
        </TouchableOpacity>
      </View>
    )
  }

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 16,
      backgroundColor: theme.colors.cardBackground,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    avatar: {
      width: 60,
      height: 60,
      borderRadius: 30,
      borderWidth: 0,
    },
    userInfo: {
      flex: 1,
      marginLeft: 12,
    },
    name: {
      fontSize: 17,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    email: {
      fontSize: 15,
      color: theme.colors.textSecondary,
    },
    logoutButton: {
      padding: 4,
    },
    logoutIcon: {
      width: 20,
      height: 20,
    },
    // Loading states
    loadingAvatar: {
      width: 60,
      height: 60,
      borderRadius: 30,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    loadingName: {
      width: 120,
      height: 17,
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 2,
      marginBottom: 6,
    },
    loadingEmail: {
      width: 160,
      height: 15,
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 2,
    },
  })
