import React, { FC } from "react"
import { Text, View, TouchableOpacity, ScrollView } from "react-native"
import { useRouter } from "expo-router"
import { PagerIcon } from "../../components"
import { makeStyles } from "./styles"
import { useSafeAreaInsets } from "react-native-safe-area-context"

import { getMessaging, requestPermission } from "@react-native-firebase/messaging"

import { useAppTheme } from "../../theme"

export const SecondScene: FC = () => {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)
    const AnimatedHeader = () => {
      const insets = useSafeAreaInsets()
      return (
        <View style={[styles.headerStyle, { paddingTop: Math.max(insets.top, 16) }]}>
          <PagerIcon fill={theme.colors.primary} height={48} width={48} />
          <Text style={styles.title}>RFSPager</Text>
        </View>
      )
    }

    return (
      <View style={styles.container}>
        <AnimatedHeader />
        <ScrollView
          style={styles.scrollView}
          contentContainerStyle={styles.contentContainerStyle}
          showsVerticalScrollIndicator={false}
        >
          <View style={styles.contentCard}>
            <View style={styles.topBar} />
            <Text style={styles.headingText}>Real-Time Notifications</Text>
            <Text style={styles.subHeading}>
              Receive instant alerts for incidents in your area. Subscribe to specific capcodes to
              filter notifications relevant to your role or location.
            </Text>
            <View style={styles.notificationAnimation} />

            <View style={styles.premiumNotice}>
              <Text style={styles.premiumNoticeText}>
                Critical for emergency response coordination
              </Text>
            </View>

            <View style={styles.swipableDotsContainer}>
              <View style={styles.dotStyle} />
              <View style={[styles.dotStyle, styles.dotActive]} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
            </View>
            <TouchableOpacity
              style={styles.nextButton}
              onPress={() => {
                requestPermission(getMessaging())
                  .finally(() => router.push("/(onboarding)/third"))
              }}
            >
              <Text style={styles.nextLabel}>Enable Notifications</Text>
            </TouchableOpacity>
          </View>
        </ScrollView>
      </View>
    )
  }
