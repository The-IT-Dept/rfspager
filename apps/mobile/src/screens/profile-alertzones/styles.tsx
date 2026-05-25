/**
 * styles for alertzone screens
 * @format
 */

import { StyleSheet } from "react-native"
import { AppTheme } from "../../theme/rfspager"

export const makestyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },

    // Main container sections
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
    sectionTitle: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    sectionSubtitle: {
      fontSize: 14,
      color: theme.colors.textSecondary,
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
      width: 100,
    },
    dataValue: {
      fontSize: 15,
      color: theme.colors.textSecondary,
      flex: 1,
    },

    // Zones container
    zonesContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    zonesHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    zonesTitle: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },

    // Empty state
    emptyContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    emptyHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    emptyTitle: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    emptyContent: {
      paddingHorizontal: 16,
      paddingVertical: 16,
    },
    emptyText: {
      fontSize: 15,
      color: theme.colors.textSecondary,
      lineHeight: 22,
      textAlign: "center",
    },

    // Create screen specific styles
    createContainer: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    formContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    formHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    formTitle: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    inputRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    inputRowLast: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
    },
    inputLabel: {
      fontSize: 15,
      fontWeight: "400",
      color: theme.colors.textPrimary,
      width: 80,
    },
    textInput: {
      flex: 1,
      fontSize: 15,
      color: theme.colors.textPrimary,
      paddingVertical: 8,
      borderBottomWidth: 1,
      borderBottomColor: "transparent",
    },
    textInputError: {
      flex: 1,
      fontSize: 15,
      color: theme.colors.textPrimary,
      paddingVertical: 8,
      borderBottomWidth: 1,
      borderBottomColor: theme.colors.error,
    },
    errorRow: {
      paddingHorizontal: 16,
      paddingTop: 8,
      paddingBottom: 12,
    },
    errorText: {
      color: theme.colors.error,
      fontSize: 13,
      marginLeft: 80,
    },

    // Slider section
    sliderContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    sliderHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    sliderTitle: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    radiusValue: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.primary,
    },
    sliderContent: {
      paddingHorizontal: 16,
      paddingVertical: 16,
    },
    slider: {
      width: "100%",
      height: 40,
    },
    sliderLabels: {
      flexDirection: "row",
      justifyContent: "space-between",
      marginTop: 8,
    },
    sliderLabel: {
      fontSize: 13,
      color: theme.colors.textSecondary,
    },

    // Map section
    mapContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
      height: 400,
    },
    mapContainerError: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
      height: 400,
      borderWidth: 1,
      borderColor: theme.colors.error,
    },
    mapHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    mapTitle: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 4,
    },
    mapDescription: {
      fontSize: 13,
      color: theme.colors.textSecondary,
    },
    mapDescriptionError: {
      fontSize: 13,
      color: theme.colors.error,
      fontWeight: "500",
    },
    map: {
      flex: 1,
    },

    // Action buttons
    actionsContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      marginBottom: 20,
      overflow: "hidden",
    },
    actionButton: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      paddingVertical: 16,
      paddingHorizontal: 16,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    actionButtonLast: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      paddingVertical: 16,
      paddingHorizontal: 16,
    },
    saveButton: {
      backgroundColor: theme.colors.primary,
    },
    discardButton: {
      backgroundColor: theme.colors.cardBackground,
    },
    saveButtonText: {
      fontSize: 17,
      fontWeight: "600",
      color: "white",
    },
    discardButtonText: {
      fontSize: 17,
      fontWeight: "400",
      color: theme.colors.error,
    },
  })
