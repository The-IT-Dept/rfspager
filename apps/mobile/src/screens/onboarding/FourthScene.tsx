import React, { FC, useState } from "react"
import { Text, Image, View, Linking, TouchableOpacity, ScrollView } from "react-native"
import { useRouter } from "expo-router"
import { Ionicons } from "@expo/vector-icons"
import { PagerIcon } from "../../components"
import { makeStyles } from "./styles"
import { useSafeAreaInsets } from "react-native-safe-area-context"
import { Images } from "../../constants"
import { useAppTheme } from "../../theme"

interface CheckboxProps {
  checked: boolean
  onPress: () => void
  color: string
}

function CheckboxItem({ checked, onPress, color }: CheckboxProps) {
  return (
    <TouchableOpacity onPress={onPress} hitSlop={8} style={{ padding: 4 }}>
      <Ionicons
        name={checked ? "checkbox" : "square-outline"}
        size={24}
        color={checked ? color : "#8E8E93"}
      />
    </TouchableOpacity>
  )
}

export const FourthScene: FC = () => {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)
    const [checkTerms, setCheckTerms] = useState(false)
    const [checkPrivacy, setCheckPrivacy] = useState(false)

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
            <Text style={styles.headingText}>Important Disclaimer</Text>
            <Text style={styles.disclaimerText}>
              • Use this service for situational awareness and information purposes only{"\n"}•
              Never rely solely on this service during emergency situations{"\n"}• Always follow
              official emergency procedures and protocols{"\n"}• Service availability and accuracy
              cannot be guaranteed{"\n"}• Report emergencies through official channels (000)
            </Text>
            <View style={styles.shieldAnimation} />
            <View style={styles.swipableDotsContainer}>
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
              <View style={[styles.dotStyle, styles.dotActive]} />
              <View style={styles.dotStyle} />
              <View style={styles.dotStyle} />
            </View>
          </View>

          <View style={styles.disclaimerSection}>
            <Text style={styles.onboard4bottomText}>
              We take privacy seriously and filter personal information from messages. If you notice
              any concerns, contact us at abuse@rfspager.app with the message ID.
            </Text>
            <Image source={Images.downArrow} style={styles.downArrow} />
          </View>

          <View style={styles.legalSection}>
            <View style={styles.legalRow}>
              <TouchableOpacity
                onPress={() => Linking.openURL("https://rfspager.app/terms-of-service")}
              >
                <Text style={styles.agreeConditionsTitle}>Terms and Conditions</Text>
              </TouchableOpacity>
              <View style={styles.checkBoxContainer}>
                <CheckboxItem
                  checked={checkTerms}
                  onPress={() => setCheckTerms(!checkTerms)}
                  color={theme.colors.primary}
                />
                <Text style={styles.agreeConditionsSubtitle}>
                  I agree with terms and conditions
                </Text>
              </View>
            </View>

            <View style={styles.legalRow}>
              <TouchableOpacity
                onPress={() => Linking.openURL("https://rfspager.app/privacy-policy")}
              >
                <Text style={styles.agreeConditionsTitle}>Privacy Policy</Text>
              </TouchableOpacity>
              <View style={styles.checkBoxContainer}>
                <CheckboxItem
                  checked={checkPrivacy}
                  onPress={() => setCheckPrivacy(!checkPrivacy)}
                  color={theme.colors.primary}
                />
                <Text style={styles.agreeConditionsSubtitle}>I agree with Privacy Policy</Text>
              </View>
            </View>
          </View>

          <View style={styles.continueButtonSection}>
            <Text style={styles.clickToContinue}>
              Review premium features before completing setup
            </Text>
            <TouchableOpacity
              style={[
                styles.continueButton,
                (!checkTerms || !checkPrivacy) && styles.continueButtonDisabled,
              ]}
              onPress={() => router.push("/(onboarding)/fifth")}
              disabled={!checkTerms || !checkPrivacy}
            >
              <Text style={styles.continueLabel}>Continue</Text>
            </TouchableOpacity>
          </View>
        </ScrollView>
      </View>
    )
  }
