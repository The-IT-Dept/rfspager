import React, { FC } from "react"
import { View, Text, Image, TouchableOpacity, ScrollView } from "react-native"
import { useRouter } from "expo-router"
import { useSafeAreaInsets } from "react-native-safe-area-context"

import { PagerIcon } from "../../components"
import { Images } from "../../constants"
import { makeStyles } from "./styles"
import { useAppTheme } from "../../theme"

export const FirstScene: FC = () => {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)
    const RenderHeader = () => {
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
        <RenderHeader />
        <ScrollView
          style={styles.scrollView}
          contentContainerStyle={styles.contentContainerStyle}
          showsVerticalScrollIndicator={false}
        >
          <View style={styles.contentCard}>
            <View style={styles.topBar} />
            <Text style={styles.headingText}>Welcome to RFSPager</Text>
            <Text style={styles.subHeading}>
              Professional emergency services monitoring for first responders and the community.
              Stay informed with real-time incident notifications from the Rural Fire Service.
            </Text>
            <Image source={Images.welcome} resizeMode={"contain"} style={styles.welcomeImage} />

            <View style={styles.premiumNotice}>
              <Text style={styles.premiumNoticeText}>
                Trusted by emergency services professionals across NSW
              </Text>
            </View>

            <View style={styles.swipableDotsContainer}>
              <View style={[styles.dotStyle, styles.dotActive]} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
            </View>
            <TouchableOpacity
              style={styles.nextButton}
              onPress={() => router.push("/(onboarding)/second")}
            >
              <Text style={styles.nextLabel}>Get Started</Text>
            </TouchableOpacity>
          </View>
        </ScrollView>
      </View>
    )
  }
