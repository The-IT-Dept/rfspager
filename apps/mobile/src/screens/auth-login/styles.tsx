/**
 * loginscreen styles
 * @format
 */

import { StyleSheet } from "react-native"
import { AppTheme } from "../../theme"

export const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    headerSection: {
      backgroundColor: theme.colors.cardBackground,
      alignItems: "center",
      paddingVertical: 40,
      paddingHorizontal: 20,
    },
    image: {
      width: 80,
      height: 80,
      marginBottom: 16,
    },
    loginText: {
      fontSize: 28,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      textAlign: "center",
    },
    contentWrapper: {
      flex: 1,
      paddingHorizontal: 16,
      paddingTop: 20,
    },
    formSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
      marginBottom: 20,
    },
    inputContainer: {
      marginBottom: 16,
    },
    textInput: {
      height: 50,
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 0,
      borderWidth: 1,
      borderColor: theme.colors.border,
      paddingHorizontal: 16,
      fontSize: 16,
      color: theme.colors.textPrimary,
    },
    textInputError: {
      borderColor: theme.colors.error,
    },
    inputLabel: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 8,
    },
    errorText: {
      fontSize: 13,
      color: theme.colors.error,
      marginTop: 4,
    },
    passwordContainer: {
      position: "relative",
    },
    forgotPassword: {
      alignSelf: "flex-end",
      paddingVertical: 8,
    },
    forgot: {
      color: theme.colors.interactive,
      fontSize: 14,
      fontWeight: "400",
    },
    loginButton: {
      backgroundColor: theme.colors.primary,
      height: 50,
      justifyContent: "center",
      alignItems: "center",
      marginTop: 20,
    },
    loginButtonDisabled: {
      backgroundColor: theme.colors.borderSecondary,
    },
    loginLabel: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.white,
    },
    socialSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
      marginBottom: 20,
    },
    loginWithText: {
      fontSize: 14,
      color: theme.colors.textTertiary,
      textAlign: "center",
      marginBottom: 20,
    },
    imageButtonContainer: {
      flexDirection: "row",
      justifyContent: "space-around",
    },
    imageButton: {
      backgroundColor: theme.colors.backgroundTertiary,
      borderWidth: 1,
      borderColor: theme.colors.border,
      height: 60,
      width: 60,
      justifyContent: "center",
      alignItems: "center",
      borderRadius: 8,
    },
    iconImage: {
      height: 24,
      width: 24,
    },
    bottomSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
    },
    bottomTextContainer: {
      flexDirection: "row",
      justifyContent: "center",
      alignItems: "center",
    },
    newToRFS: {
      color: theme.colors.textTertiary,
      fontSize: 15,
      fontWeight: "400",
    },
    signup: {
      color: theme.colors.interactive,
      fontSize: 15,
      fontWeight: "600",
    },
    logoutMessage: {
      color: theme.colors.error,
      fontSize: 14,
      textAlign: "center",
      marginBottom: 16,
    },
    loading: {
      alignItems: "center",
      backgroundColor: "rgba(0,0,0,0.5)",
      bottom: 0,
      justifyContent: "center",
      left: 0,
      position: "absolute",
      right: 0,
      top: 0,
    },
  })
