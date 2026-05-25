import React from "react"
import { Text, View, TouchableOpacity, ScrollView } from "react-native"
import { useSafeAreaInsets } from "react-native-safe-area-context"

import { PagerIcon } from "../../components"
import { makeStyles } from "./styles"
import { usePreferencesStore } from "../../stores"
import { useAppTheme } from "../../theme"

type Props = {
  animatedValue: any
  nextScene: () => void
}

export const SixthScene = (props: Props) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const setOnboarded = usePreferencesStore((s) => s.setOnboarded)

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
          <Text style={styles.headingText}>You're All Set!</Text>
          <Text style={styles.subHeading}>
            RFSPager is now configured for your device. Start monitoring emergency communications in
            your area and stay informed about incidents as they develop.
          </Text>

          <View style={styles.premiumNotice}>
            <Text style={styles.premiumNoticeText}>
              Explore premium features anytime from Settings → Manage Subscription
            </Text>
          </View>

          <View style={styles.swipableDotsContainer}>
            <View style={styles.dotStyle} />
            <View style={styles.dotStyle} />
            <View style={styles.dotStyle} />
            <View style={styles.dotStyle} />
            <View style={styles.dotStyle} />
            <View style={[styles.dotStyle, styles.dotActive]} />
          </View>

          <TouchableOpacity style={styles.nextButton} onPress={() => setOnboarded(true)}>
            <Text style={styles.nextLabel}>Start Using RFSPager</Text>
          </TouchableOpacity>
        </View>
      </ScrollView>
    </View>
  )
}
