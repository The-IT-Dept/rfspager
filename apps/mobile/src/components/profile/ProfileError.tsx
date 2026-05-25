import React from "react"
import { View, StyleSheet, Text } from "react-native"
import { Button } from "../button/button"
import { useAppTheme, AppTheme } from "../../theme"

interface ProfileErrorProps {
  error: string
  onRetry: () => void
}

export const ProfileError: React.FC<ProfileErrorProps> = ({ error, onRetry }) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  return (
    <View style={styles.container}>
      <Text style={styles.errorTitle}>Something went wrong</Text>
      <Text style={styles.errorMessage}>{error}</Text>
      <Button mode="contained" onPress={onRetry} style={styles.retryButton}>
        Try Again
      </Button>
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      paddingHorizontal: 32,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    errorTitle: {
      fontSize: 22,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 12,
      textAlign: "center",
    },
    errorMessage: {
      fontSize: 17,
      color: theme.colors.textSecondary,
      textAlign: "center",
      lineHeight: 22,
      marginBottom: 32,
    },
    retryButton: {
      minWidth: 120,
      backgroundColor: theme.colors.primary,
    },
  })
