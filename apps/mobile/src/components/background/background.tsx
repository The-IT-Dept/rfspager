import * as React from "react"
import { View, KeyboardAvoidingView, StyleSheet } from "react-native"
import { AppTheme, useAppTheme } from "../../theme"

export interface BackgroundProps {
  children: React.ReactNode
  containersStyle: any
  containerStyle: any
}

/**
 * Describe your component here
 */
export const Background = function Background(props: BackgroundProps) {
  const { children, containersStyle, containerStyle } = props
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  return (
    <View style={styles.background}>
      <KeyboardAvoidingView
        style={[styles.container, containersStyle, containerStyle]}
        behavior="padding"
        enabled
      >
        {children}
      </KeyboardAvoidingView>
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    background: {
      backgroundColor: theme.colors.surface,
      flex: 1,
      width: "100%",
    },
    container: {
      alignItems: "center",
      alignSelf: "center",
      flex: 1,
      justifyContent: "center",
      maxWidth: 340,
      padding: 20,
      width: "100%",
    },
    scrollView: {
      alignSelf: "center",
      flex: 1,
      width: "100%",
    },
    scrollViewContentContainer: {
      alignItems: "center",
      justifyContent: "center",
    },
  })
