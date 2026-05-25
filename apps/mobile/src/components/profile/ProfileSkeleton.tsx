import React from "react"
import { View, StyleSheet } from "react-native"
import { useAppTheme, AppTheme } from "../../theme"

export const ProfileSkeleton = (() => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  return (
    <View style={styles.container}>
      {/* Header skeleton */}
      <View style={styles.headerSkeleton}>
        <View style={styles.avatarSkeleton} />
        <View style={styles.userInfoSkeleton}>
          <View style={styles.nameSkeleton} />
          <View style={styles.emailSkeleton} />
        </View>
      </View>

      {/* Settings sections skeleton */}
      {[1, 2, 3, 4].map((i) => (
        <View key={i} style={styles.sectionSkeleton}>
          <View style={styles.sectionHeaderSkeleton} />
          {[1, 2, 3].map((j) => (
            <View key={j} style={styles.settingItemSkeleton} />
          ))}
        </View>
      ))}
    </View>
  )
})

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    headerSkeleton: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      backgroundColor: theme.colors.cardBackground,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    avatarSkeleton: {
      width: 60,
      height: 60,
      borderRadius: 30,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    userInfoSkeleton: {
      flex: 1,
      marginLeft: 12,
    },
    nameSkeleton: {
      width: "60%",
      height: 17,
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 2,
      marginBottom: 6,
    },
    emailSkeleton: {
      width: "80%",
      height: 15,
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 2,
    },
    sectionSkeleton: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginVertical: 16,
      overflow: "hidden",
    },
    sectionHeaderSkeleton: {
      height: 32,
      backgroundColor: theme.colors.backgroundTertiary,
      marginBottom: 6,
    },
    settingItemSkeleton: {
      height: 44,
      backgroundColor: theme.colors.backgroundTertiary,
      marginBottom: 0.5,
      borderRadius: 10,
    },
  })
