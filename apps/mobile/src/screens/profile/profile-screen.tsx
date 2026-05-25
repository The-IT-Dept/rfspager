import React, { FC, useEffect, useState, useCallback } from "react"
import { ScrollView, StyleSheet, View, Alert, Linking, RefreshControl } from "react-native"
import { nativeApplicationVersion } from "expo-application"
import { useRouter } from "expo-router"

import {
  SettingsSection,
  ProfileSkeleton,
  ProfileError,
  SettingItem,
  SubscriptionModal,
} from "../../components"
import { useAuthStore, useValidSubscription } from "../../stores"
import { usePreferencesStore } from "../../stores"
import { getInstallationId } from "../../utils/installation"
import { logEvent } from "../../utils/analytics"
import { Images } from "../../constants"
import { useAppTheme, AppTheme } from "../../theme"

type LoadingState = "loading" | "success" | "error"

export const ProfileScreen: FC = function ProfileScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()

  const validSubscription = useValidSubscription()
  const resetApp = useAuthStore((s) => s.resetApp)
  const refreshUser = useAuthStore((s) => s.refreshUser)
  const upsertInstallation = useAuthStore((s) => s.upsertInstallation)
  const ensureFreshUser = useAuthStore((s) => s.ensureFreshUser)

  const darkMode = usePreferencesStore((s) => s.darkMode)
  const showSearchFields = usePreferencesStore((s) => s.showSearchFields)
  const pagerFilter = usePreferencesStore((s) => s.pagerFilter)
  const pagerShowUnknown = usePreferencesStore((s) => s.pagerShowUnknown)
  const mapPageSize = usePreferencesStore((s) => s.mapPageSize)
  const clusterMapIcons = usePreferencesStore((s) => s.clusterMapIcons)
  const mapShowHR = usePreferencesStore((s) => s.mapShowHR)
  const mapShowUpdates = usePreferencesStore((s) => s.mapShowUpdates)
  const setDarkMode = usePreferencesStore((s) => s.setDarkMode)
  const setShowSearchFields = usePreferencesStore((s) => s.setShowSearchFields)
  const setPagerFilter = usePreferencesStore((s) => s.setPagerFilter)
  const setPagerShowUnknown = usePreferencesStore((s) => s.setPagerShowUnknown)
  const setClusterMapIcons = usePreferencesStore((s) => s.setClusterMapIcons)
  const setMapShowHR = usePreferencesStore((s) => s.setMapShowHR)
  const setMapShowUpdates = usePreferencesStore((s) => s.setMapShowUpdates)
  const [loadingState, setLoadingState] = useState<LoadingState>("loading")
  const [error, setError] = useState<string>("")
  const [refreshing, setRefreshing] = useState(false)
  const [subscriptionModalVisible, setSubscriptionModalVisible] = useState(false)

  const initializeProfile = useCallback(async () => {
    try {
      setLoadingState("loading")
      setError("")

      // Run operations in parallel for better performance
      await Promise.all([upsertInstallation(), ensureFreshUser()])

      setLoadingState("success")
    } catch (err) {
      console.error("Profile initialization error:", err)
      setError(err?.message || "Failed to load profile")
      setLoadingState("error")
    }
  }, [upsertInstallation])

  const onRefresh = useCallback(async () => {
    setRefreshing(true)
    try {
      await Promise.all([upsertInstallation()])
      // refreshUser returns a promise
      refreshUser().finally(() => setRefreshing(false))
    } catch (err) {
      console.error("Refresh error:", err)
      setRefreshing(false)
    }
  }, [upsertInstallation, refreshUser])

  useEffect(() => {
    initializeProfile()
  }, [initializeProfile])

  const handlePremiumFeature = useCallback(
    (action: () => void) => {
      if (!validSubscription) {
        setSubscriptionModalVisible(true)
      } else {
        action()
      }
    },
    [validSubscription],
  )

  const handleNeedHelp = useCallback(async () => {
    try {
      const installationID = await getInstallationId()
      Alert.alert(
        "Need Help?",
        `Your Support Token is ${installationID}. Please send a screenshot of this screen to hello@rfspager.app with a description of your issue.`,
      )
      await logEvent("need_help")
    } catch (err) {
      Alert.alert("Error", "Failed to get support token")
    }
  }, [])

  const handleReset = useCallback(() => {
    Alert.alert(
      "Reset App",
      "This will clear all data and return you to the onboarding screen. This action cannot be undone.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Reset",
          style: "destructive",
          onPress: () => {
            resetApp()
          },
        },
      ],
    )
  }, [resetApp])

  // Settings configuration
  const accountSettings: SettingItem[] = [
    {
      id: "my-account",
      title: "My Account",
      type: "navigation",
      onPress: () => router.push("/(tabs)/settings/account"),
    },
    {
      id: "subscription",
      title: "Manage Subscription",
      type: "navigation",
      onPress: () => router.push("/(tabs)/settings/subscription"),
    },
  ]

  const pagerSettings: SettingItem[] = [
    {
      id: "alert-zones",
      title: "Alert Zones",
      type: "navigation",
      isPremium: true,
      onPress: () => handlePremiumFeature(() => router.push("/(tabs)/settings/alertzones")),
    },
    {
      id: "capcodes",
      title: "Capcodes",
      type: "navigation",
      onPress: () => router.push("/(tabs)/settings/capcodes"),
    },
    {
      id: "filter-capcodes",
      title: "Only Show Selected Capcodes",
      type: "toggle",
      isPremium: true,
      value: pagerFilter,
      onToggle: () =>
        handlePremiumFeature(() => setPagerFilter(!pagerFilter)),
    },
    {
      id: "show-unknown",
      title: "Show Unknown Capcodes",
      type: "toggle",
      isPremium: true,
      value: pagerShowUnknown,
      onToggle: () =>
        handlePremiumFeature(() => setPagerShowUnknown(!pagerShowUnknown)),
    },
  ]

  const mapSettings: SettingItem[] = [
    {
      id: "cluster-icons",
      title: "Cluster Map Icons",
      type: "toggle",
      value: clusterMapIcons,
      onToggle: () => setClusterMapIcons(!clusterMapIcons),
    },
    {
      id: "major-updates",
      title: "Show Major Fire Updates",
      type: "toggle",
      value: mapShowUpdates,
      onToggle: () => setMapShowUpdates(!mapShowUpdates),
    },
    {
      id: "hazard-burns",
      title: "Show Hazard Reduction Burns",
      type: "toggle",
      value: mapShowHR,
      onToggle: () => setMapShowHR(!mapShowHR),
    },
    {
      id: "map-page-size",
      title: "Number of Messages",
      type: "info",
      value: mapPageSize?.toString() || "100",
    },
  ]

  const generalSettings: SettingItem[] = [
    {
      id: "dark-mode",
      title: "Dark Mode",
      type: "toggle",
      value: darkMode,
      onToggle: () => setDarkMode(!darkMode),
    },
    {
      id: "search-fields",
      title: "Show Search Fields",
      type: "toggle",
      value: showSearchFields,
      onToggle: () => setShowSearchFields(!showSearchFields),
    },
  ]

  const aboutSettings: SettingItem[] = [
    {
      id: "discord",
      title: "Join us on Discord",
      type: "navigation",
      onPress: async () => {
        await logEvent("discord")
        await Linking.openURL("https://rfspager.app/discord")
      },
    },
    {
      id: "website",
      title: "RFSPager.app",
      type: "navigation",
      onPress: async () => {
        await logEvent("rfspager_app")
        await Linking.openURL("https://rfspager.app")
      },
    },
    {
      id: "company",
      title: "The IT Dept Pty Ltd",
      type: "navigation",
      onPress: async () => {
        await logEvent("the_it_dept_website")
        await Linking.openURL("https://theitdept.au")
      },
    },
  ]

  const supportSettings: SettingItem[] = [
    {
      id: "help",
      title: "Need Help?",
      type: "navigation",
      onPress: handleNeedHelp,
    },
    {
      id: "version",
      title: "App Version",
      type: "info",
      value: nativeApplicationVersion,
    },
    {
      id: "reset",
      title: "Reset App",
      type: "navigation",
      onPress: handleReset,
    },
  ]

  if (loadingState === "loading") {
    return <ProfileSkeleton />
  }

  if (loadingState === "error") {
    return <ProfileError error={error} onRetry={initializeProfile} />
  }

  return (
    <View style={styles.container}>
      <ScrollView
        style={styles.scrollView}
        showsVerticalScrollIndicator={false}
        contentInsetAdjustmentBehavior="automatic"
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={onRefresh}
            tintColor={theme.colors.primary}
          />
        }
      >
        <SettingsSection title="Account" icon={Images.account} items={accountSettings} />

        <SettingsSection title="Pager" icon={Images.pager} items={pagerSettings} />

        <SettingsSection title="Map" icon={Images.map} items={mapSettings} />

        <SettingsSection title="General" icon={Images.general} items={generalSettings} />

        <SettingsSection title="About" icon={Images.about} items={aboutSettings} />

        <SettingsSection title="Support" icon={Images.other} items={supportSettings} />
      </ScrollView>

      <SubscriptionModal
        visible={subscriptionModalVisible}
        onDismiss={() => setSubscriptionModalVisible(false)}
      />
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    scrollView: {
      flex: 1,
    },
    bottomPadding: {
      height: 100,
    },
  })
