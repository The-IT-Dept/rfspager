import React, { FC } from "react"
import { Text, View, TouchableOpacity, ScrollView } from "react-native"
import { useRouter } from "expo-router"
import { requestTrackingPermission } from "react-native-tracking-transparency"
import { useSafeAreaInsets } from "react-native-safe-area-context"

import { PagerIcon } from "../../components"
import { makeStyles } from "./styles"
import { useAppTheme } from "../../theme"

export const ThirdScene: FC = () => {
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
            <Text style={styles.headingText}>Privacy & Personalization</Text>
            <Text style={styles.subHeading}>
              To keep RFSPager free for emergency services, we show personalized advertisements.
              This helps us maintain and improve the service for the community.
            </Text>
            <View style={styles.adsenseAnimation} />

            <View style={styles.premiumNotice}>
              <Text style={styles.premiumNoticeText}>
                Premium subscribers enjoy an ad-free experience
              </Text>
            </View>

            <View style={styles.swipableDotsContainer}>
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={[styles.dotStyle, styles.dotActive]} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
            </View>
            <TouchableOpacity
              style={styles.nextButton}
              onPress={() =>
                requestTrackingPermission().finally(() => router.push("/(onboarding)/fourth"))
              }
            >
              <Text style={styles.nextLabel}>Continue</Text>
            </TouchableOpacity>
          </View>
        </ScrollView>
      </View>
    )
  }
