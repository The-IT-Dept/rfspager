import * as React from "react"
import { StyleProp, StyleSheet, TextStyle } from "react-native"
import { Text } from "react-native"
import { AppTheme, useAppTheme } from "../../theme"

export interface HeaderProps {
  /**
   * An optional style override useful for padding & margin.
   */
  style?: StyleProp<TextStyle>
  children: any
}

/**
 * Describe your component here
 */
export const Header = function Header({ style, ...rest }: HeaderProps) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  // `style` is merged rather than spread through: spreading props after
  // style={styles.header} let a caller's style replace the component's own
  // styling outright instead of layering on top of it.
  return <Text style={[styles.header, style]} {...rest} />
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
