import React, { FC } from "react"
import { Text, View, TouchableOpacity, ScrollView } from "react-native"
import { useSafeAreaInsets } from "react-native-safe-area-context"
import { Ionicons } from "@expo/vector-icons"

import { PagerIcon } from "../../components"
import { makeStyles } from "./styles"
import { PREMIUM_FEATURES } from "../../config/premium-features"
import { usePreferencesStore } from "../../stores"
import { useAppTheme } from "../../theme"

export const FifthScene: FC = () => {
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
            <Text style={styles.headingText}>Premium Features</Text>
            <Text style={styles.subHeading}>
              Unlock advanced capabilities to enhance your emergency monitoring experience
            </Text>

            <View style={styles.premiumFeaturesList}>
              {PREMIUM_FEATURES.slice(0, 4).map((feature, index) => (
                <View key={feature.id} style={styles.premiumFeatureItem}>
                  <View style={styles.premiumFeatureIcon}>
                    <Ionicons name={feature.icon as any} size={20} color={theme.colors.primary} />
                  </View>
                  <View style={styles.premiumFeatureContent}>
                    <Text style={styles.premiumFeatureTitle}>{feature.title}</Text>
                    <Text style={styles.premiumFeatureDescription}>{feature.description}</Text>
                  </View>
                </View>
              ))}
            </View>

            <View style={styles.premiumNotice}>
              <Text style={styles.premiumNoticeText}>
                You can explore all features during your free trial and upgrade anytime from
                Settings
              </Text>
            </View>

            <View style={styles.swipableDotsContainer}>
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={[styles.dotStyle, styles.dotActive]} />
            </View>

            <TouchableOpacity style={styles.nextButton} onPress={() => setOnboarded(true)}>
              <Text style={styles.nextLabel}>Continue</Text>
            </TouchableOpacity>
          </View>
        </ScrollView>
      </View>
    )
  }
