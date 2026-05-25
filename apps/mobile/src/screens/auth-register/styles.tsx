/**
 * styles for register screeen
 * @format
 */

import { StyleSheet } from "react-native"
import { AppTheme } from "../../theme/rfspager"

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
    signUpText: {
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
    socialSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
      marginBottom: 20,
    },
    imageButtonContainer: {
      flexDirection: "row",
      justifyContent: "space-around",
      marginBottom: 20,
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
    registerWithText: {
      fontSize: 14,
      color: theme.colors.textTertiary,
      textAlign: "center",
      marginBottom: 20,
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
    inputLabel: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 8,
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
    errorText: {
      fontSize: 13,
      color: theme.colors.error,
      marginTop: 4,
    },
    conditionsSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
      marginBottom: 20,
    },
    conditionsTextContainer: {
      marginBottom: 20,
    },
    row: {
      flexDirection: "row",
      flexWrap: "wrap",
      marginBottom: 4,
    },
    textStyle: {
      color: theme.colors.textTertiary,
      fontSize: 14,
      fontWeight: "400",
    },
    link: {
      color: theme.colors.interactive,
      fontSize: 14,
      fontWeight: "600",
    },
    continueButton: {
      backgroundColor: theme.colors.primary,
      height: 50,
      justifyContent: "center",
      alignItems: "center",
      marginTop: 20,
    },
    continueButtonDisabled: {
      backgroundColor: theme.colors.borderSecondary,
    },
    continueLabel: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.white,
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
    alreadyHaveAccount: {
      color: theme.colors.textTertiary,
      fontSize: 15,
      fontWeight: "400",
    },
    loginLink: {
      color: theme.colors.interactive,
      fontSize: 15,
      fontWeight: "600",
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
