import * as React from "react"
import { StyleProp, StyleSheet, ViewStyle } from "react-native"
import { Text } from "react-native"
import { AppTheme, useAppTheme } from "../../theme"

export interface HeaderProps {
  /**
   * An optional style override useful for padding & margin.
   */
  style?: StyleProp<ViewStyle>
  children: any
}

/**
 * Describe your component here
 */
export const Header = function Header(props: HeaderProps) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  return <Text style={styles.header} {...props} />
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    header: {
      color: theme.colors.primary,
      fontSize: 21,
      fontWeight: "bold",
      paddingVertical: 12,
    },
  })
