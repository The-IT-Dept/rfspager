/**
 *styles for subscription screen
 * @format
 */

import { StyleSheet } from "react-native"
import { AppTheme } from "../../theme"

export const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    blank: { flex: 1 },

    // Container styles
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },

    // Main subscription status section
    mainContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    headerRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    iconContainer: {
      width: 20,
      height: 20,
      borderRadius: 3,
      justifyContent: "center",
      alignItems: "center",
      marginRight: 12,
    },
    headerContent: {
      flex: 1,
    },
    statusTitle: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    statusSubtitle: {
      fontSize: 14,
      color: theme.colors.textSecondary,
    },
    expiryDate: {
      fontSize: 13,
      color: theme.colors.textSecondary,
      fontWeight: "400",
    },
    dataRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    dataLabel: {
      fontSize: 15,
      fontWeight: "400",
      color: theme.colors.textPrimary,
      width: 80,
    },
    dataValue: {
      fontSize: 15,
      color: theme.colors.textSecondary,
      flex: 1,
    },

    // Section containers
    sectionContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    sectionHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    sectionTitle: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },

    // Feature rows
    featureRow: {
      flexDirection: "row",
      alignItems: "flex-start",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    featureRowLast: {
      flexDirection: "row",
      alignItems: "flex-start",
      paddingHorizontal: 16,
      paddingVertical: 12,
    },
    featureIcon: {
      width: 24,
      height: 24,
      justifyContent: "center",
      alignItems: "center",
      marginRight: 12,
      marginTop: 2,
    },
    featureContent: {
      flex: 1,
    },
    featureTitle: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 4,
    },
    featureDescription: {
      fontSize: 14,
      color: theme.colors.textSecondary,
      lineHeight: 20,
    },

    // Plans section
    plansContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    plansHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    plansTitle: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    planRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 16,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    planRowRecommended: {
      backgroundColor: theme.colors.primary + "10",
    },
    planContent: {
      flex: 1,
    },
    planTitle: {
      fontSize: 17,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 4,
    },
    planDescription: {
      fontSize: 14,
      color: theme.colors.textSecondary,
    },
    planPrice: {
      alignItems: "flex-end",
    },
    priceText: {
      fontSize: 17,
      fontWeight: "600",
      color: theme.colors.primary,
      marginBottom: 2,
    },
    savingsText: {
      fontSize: 11,
      fontWeight: "600",
      color: theme.colors.primary,
      backgroundColor: theme.colors.primary + "15",
      paddingHorizontal: 6,
      paddingVertical: 2,
      borderRadius: 4,
      overflow: "hidden",
    },
    restoreButton: {
      paddingHorizontal: 16,
      paddingVertical: 16,
      alignItems: "center",
    },
    restoreText: {
      fontSize: 16,
      color: theme.colors.primary,
      fontWeight: "400",
    },

    // Legal section
    legalContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      marginBottom: 20,
      paddingHorizontal: 16,
      paddingVertical: 16,
    },
    legalText: {
      fontSize: 13,
      color: theme.colors.textSecondary,
      lineHeight: 18,
      textAlign: "justify",
      marginBottom: 12,
    },
    legalLinksRow: {
      flexDirection: "row",
      justifyContent: "center",
      alignItems: "center",
    },
    legalLink: {
      fontSize: 13,
      color: theme.colors.primary,
      fontWeight: "400",
    },
    legalSeparator: {
      fontSize: 13,
      color: theme.colors.textSecondary,
      marginHorizontal: 8,
    },

    // Loading portal
    portalView: {
      flex: 1,
      width: "100%",
      height: "100%",
      backgroundColor: "rgba(0, 0, 0, 0.75)",
      justifyContent: "center",
      alignItems: "center",
    },
  })
